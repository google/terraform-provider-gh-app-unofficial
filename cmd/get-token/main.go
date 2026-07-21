package main

import (
	"bufio"
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// loadEnv parses a local .env file and sets env variables if they aren't already set.
func loadEnv() {
	file, err := os.Open(".env")
	if err != nil {
		return // ignore error if .env doesn't exist
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.Trim(strings.TrimSpace(parts[1]), `"'`)
		if os.Getenv(key) == "" {
			os.Setenv(key, val)
		}
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Error reading .env file: %v\n", err)
	}
}

// generateJWT creates a signed GitHub App JWT token.
func generateJWT(appID string, privateKeyPEM []byte) (string, error) {
	block, _ := pem.Decode(privateKeyPEM)
	if block == nil {
		return "", fmt.Errorf("failed to parse PEM block containing private key")
	}

	var privKey *rsa.PrivateKey
	var err error

	// Try parsing PKCS1
	privKey, err = x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		// Try parsing PKCS8
		key, err2 := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err2 != nil {
			return "", fmt.Errorf("failed to parse private key: PKCS#1 err: %v, PKCS#8 err: %v", err, err2)
		}
		var ok bool
		privKey, ok = key.(*rsa.PrivateKey)
		if !ok {
			return "", fmt.Errorf("private key is not an RSA key")
		}
	}

	// JWT Header
	header := `{"alg":"RS256","typ":"JWT"}`
	encodedHeader := base64.RawURLEncoding.EncodeToString([]byte(header))

	// JWT Payload (Issued 60s in past, expires in 10m)
	now := time.Now().Unix()
	payload := fmt.Sprintf(`{"iss":"%s","iat":%d,"exp":%d}`, appID, now-60, now+600)
	encodedPayload := base64.RawURLEncoding.EncodeToString([]byte(payload))

	// Sign
	signingInput := encodedHeader + "." + encodedPayload
	hasher := sha256.New()
	hasher.Write([]byte(signingInput))
	hashed := hasher.Sum(nil)

	signature, err := rsa.SignPKCS1v15(rand.Reader, privKey, crypto.SHA256, hashed)
	if err != nil {
		return "", fmt.Errorf("failed to sign JWT: %v", err)
	}
	encodedSignature := base64.RawURLEncoding.EncodeToString(signature)

	return signingInput + "." + encodedSignature, nil
}

type TokenResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

// getEnvAny returns the first non-empty environment variable value from the provided keys.
func getEnvAny(keys ...string) string {
	for _, key := range keys {
		if val := os.Getenv(key); val != "" {
			return val
		}
	}
	return ""
}

func main() {
	loadEnv()

	// Parse flags with fallback to env variables
	flagAppID := flag.String("app-id", os.Getenv("GITHUB_APP_ID"), "GitHub App Client ID / Issuer ID (falls back to GITHUB_APP_ID env)")
	flagKey := flag.String("key", getEnvAny("GITHUB_APP_PRIVATE_KEY", "GH_APP_PRIVATE_KEY"), "Raw private key PEM content (falls back to GITHUB_APP_PRIVATE_KEY or GH_APP_PRIVATE_KEY env)")
	flagKeyPath := flag.String("key-path", getEnvAny("GITHUB_APP_PRIVATE_KEY_PATH", "GH_APP_PRIVATE_KEY_PATH"), "Full file path to the private key file (e.g. ~/keys/manager.pem, falls back to GITHUB_APP_PRIVATE_KEY_PATH env)")
	flagInstID := flag.String("inst-id", os.Getenv("GITHUB_APP_INSTALLATION_ID"), "GitHub App Installation ID (falls back to GITHUB_APP_INSTALLATION_ID env)")
	flagExport := flag.Bool("env-export", false, "Output format as export statement (e.g. export GITHUB_TOKEN=...)")
	flag.Parse()

	// Validation
	if *flagAppID == "" || (*flagKey == "" && *flagKeyPath == "") || *flagInstID == "" {
		fmt.Fprintf(os.Stderr, "Error: Missing required configuration.\n\n")
		fmt.Fprintf(os.Stderr, "Please specify the configuration using command flags or environment variables (.env):\n")
		fmt.Fprintf(os.Stderr, "  -app-id   / GITHUB_APP_ID               : %q\n", *flagAppID)
		fmt.Fprintf(os.Stderr, "  -key      / GITHUB_APP_PRIVATE_KEY      : [raw PEM content]\n")
		fmt.Fprintf(os.Stderr, "  -key-path / GITHUB_APP_PRIVATE_KEY_PATH : %q (expects full file path including filename)\n", *flagKeyPath)
		fmt.Fprintf(os.Stderr, "  -inst-id  / GITHUB_APP_INSTALLATION_ID  : %q\n\n", *flagInstID)
		fmt.Fprintf(os.Stderr, "Example .env file:\n")
		fmt.Fprintf(os.Stderr, "  GITHUB_APP_ID=3875173\n")
		fmt.Fprintf(os.Stderr, "  GITHUB_APP_PRIVATE_KEY_PATH=manager.pem\n")
		fmt.Fprintf(os.Stderr, "  GITHUB_APP_INSTALLATION_ID=135885315\n\n")
		os.Exit(1)
	}

	var privateKeyPEM []byte
	if *flagKey != "" {
		privateKeyPEM = []byte(*flagKey)
	} else {
		// Resolve private key file path (expects full file path including filename, e.g. ~/keys/manager.pem)
		keyPath := *flagKeyPath
		if strings.HasPrefix(keyPath, "~/") {
			home, err := os.UserHomeDir()
			if err == nil {
				keyPath = filepath.Join(home, keyPath[2:])
			}
		}

		if strings.ToLower(filepath.Ext(keyPath)) != ".pem" {
			fmt.Fprintf(os.Stderr, "Error: Invalid key path %q: private key file must have a .pem extension\n", keyPath)
			os.Exit(1)
		}

		var err error
		privateKeyPEM, err = os.ReadFile(keyPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: Failed to read private key from %s: %v\n", keyPath, err)
			os.Exit(1)
		}
	}

	// Generate JWT
	jwt, err := generateJWT(*flagAppID, privateKeyPEM)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: Failed to generate JWT: %v\n", err)
		os.Exit(1)
	}

	// Request installation access token
	url := fmt.Sprintf("https://api.github.com/app/installations/%s/access_tokens", *flagInstID)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer([]byte("{}")))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: Failed to create request: %v\n", err)
		os.Exit(1)
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: Failed to make request to GitHub API: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: Failed to read response body: %v\n", err)
		os.Exit(1)
	}

	if resp.StatusCode != http.StatusCreated {
		fmt.Fprintf(os.Stderr, "Error: GitHub API returned status %s\nResponse: %s\n", resp.Status, string(body))
		os.Exit(1)
	}

	var tokenResp TokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		fmt.Fprintf(os.Stderr, "Error: Failed to parse GitHub API response: %v\n", err)
		os.Exit(1)
	}

	if *flagExport {
		fmt.Printf("export GITHUB_TOKEN=%s\n", tokenResp.Token)
	} else {
		fmt.Println(tokenResp.Token)
	}
}
