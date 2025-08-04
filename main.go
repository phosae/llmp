package main

import (
	"bufio"
	"bytes"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"gopkg.in/yaml.v3"
)

type ModelConfig struct {
	ModelName     string        `yaml:"model_name"`
	LitellmParams LitellmParams `yaml:"litellm_params"`
}

type LitellmParams struct {
	Model   string `yaml:"model"`
	APIBase string `yaml:"api_base"`
	APIKey  string `yaml:"api_key"`
}

type Config struct {
	ModelList []ModelConfig `yaml:"model_list"`
	AuthToken string        `yaml:"auth_token,omitempty"`
}

var modelConfigs map[string]ModelConfig
var authToken string

func loadConfig(configPath string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return err
	}

	modelConfigs = make(map[string]ModelConfig)
	for _, modelConfig := range config.ModelList {
		modelConfigs[modelConfig.ModelName] = modelConfig
	}

	authToken = config.AuthToken

	// If no auth token in config, check LITELLM_MASTER_KEY env var
	if authToken == "" {
		authToken = os.Getenv("LITELLM_MASTER_KEY")
	}

	return nil
}

func isAnthropicModel(modelName string) bool {
	return strings.HasPrefix(modelName, "anthropic/")
}

func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if authToken == "" {
			next(w, r)
			return
		}

		authHeader := r.Header.Get("Authorization")
		token := strings.TrimPrefix(authHeader, "Bearer ")
		if authHeader == "" {
			authHeader = r.Header.Get("x-api-key")
			token = authHeader
		}
		if authHeader == "" {
			http.Error(w, "Authorization or x-api-key header required", http.StatusUnauthorized)
			return
		}

		if token != authToken {
			http.Error(w, "Invalid token", http.StatusUnauthorized)
			return
		}

		next(w, r)
	}
}

func proxyToUpstream(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Error reading request body", http.StatusBadRequest)
		return
	}

	modelName := gjson.GetBytes(body, "model").String()
	if modelName == "" {
		http.Error(w, "Model field is required", http.StatusBadRequest)
		return
	}

	config, exists := modelConfigs[modelName]
	if !exists {
		http.Error(w, "Model not found", http.StatusBadRequest)
		return
	}

	if !isAnthropicModel(config.LitellmParams.Model) {
		http.Error(w, "OpenAI models should use /chat/completions endpoint", http.StatusBadRequest)
		return
	}

	modifiedBody, err := sjson.SetBytes(body, "model", func(m string) string {
		if isAnthropicModel(m) {
			return strings.TrimPrefix(m, "anthropic/")
		} else {
			return strings.TrimPrefix(m, "openai/")
		}
	}(config.LitellmParams.Model))
	if err != nil {
		http.Error(w, "Error modifying request", http.StatusInternalServerError)
		return
	}

	upstreamURL := strings.TrimSuffix(config.LitellmParams.APIBase, "/") + r.URL.Path

	log.Printf("Upstream URL: %s", upstreamURL)
	log.Printf("Request body: %s", string(modifiedBody))

	isStream := gjson.GetBytes(body, "stream").Bool()

	var client *http.Client
	if isStream {
		client = &http.Client{}
	} else {
		client = &http.Client{
			Timeout: 30 * time.Second,
		}
	}

	req, err := http.NewRequest("POST", upstreamURL, bytes.NewBuffer(modifiedBody))
	if err != nil {
		http.Error(w, "Error creating request", http.StatusInternalServerError)
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "llmp-proxy/1.0")

	if config.LitellmParams.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+config.LitellmParams.APIKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "Error forwarding request", http.StatusBadGateway)
		return
	}

	log.Printf("Upstream response status: %d, headers: %v", resp.StatusCode, resp.Header)

	for key, values := range resp.Header {
		// Skip Content-Length for streaming responses to avoid conflict with Transfer-Encoding: chunked
		if isStream && strings.ToLower(key) == "content-length" {
			continue
		}
		// Skip Transfer-Encoding as Go will set it automatically for chunked responses
		if isStream && strings.ToLower(key) == "transfer-encoding" {
			continue
		}
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	defer resp.Body.Close()

	// Write the status code first
	w.WriteHeader(resp.StatusCode)

	if isStream {
		flusher, ok := w.(http.Flusher)
		if !ok {
			log.Printf("Streaming not supported")
			http.Error(w, "Streaming not supported", http.StatusInternalServerError)
			return
		}

		// Use Scanner for line-based LLM streaming (SSE format)
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 64<<10), 10<<20) // 10MB max buffer
		scanner.Split(bufio.ScanLines)

		for scanner.Scan() {
			line := scanner.Text()
			log.Printf("Streaming line: %s", line) // Debug log

			// Write the complete line with proper SSE format
			if _, writeErr := w.Write([]byte(line + "\n")); writeErr != nil {
				log.Printf("Error writing streaming line: %v", writeErr)
				return
			}
			flusher.Flush() // Flush after each complete line
		}

		// Check for scanner errors
		if err := scanner.Err(); err != nil {
			log.Printf("Scanner error in streaming response: %v", err)
		}

		log.Printf("Streaming completed")
	} else {
		if _, err := io.Copy(w, resp.Body); err != nil {
			log.Printf("Error copying response: %v", err)
		}
	}
}

func main() {
	configPath := "config.yaml"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	if err := loadConfig(configPath); err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	log.Printf("Loaded %d models from config", len(modelConfigs))

	http.HandleFunc("/v1/chat/completions", authMiddleware(proxyToUpstream))
	http.HandleFunc("/chat/completions", authMiddleware(proxyToUpstream))
	http.HandleFunc("/v1/messages", authMiddleware(proxyToUpstream))

	port := ":8400"
	log.Printf("Starting proxy server on port %s", port)

	if err := http.ListenAndServe(port, nil); err != nil {
		log.Fatal("Server failed to start:", err)
	}
}
