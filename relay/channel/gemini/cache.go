package gemini

import (
	"encoding/json"
	"fmt"
	"context"
	"time"
	"net/http"
	"one-api/common"
	"one-api/service"
	"one-api/setting/model_setting"
	"strings"
)

const GeminiCacheMinTokenThreshold = 4096

func ShouldEnableGeminiCache(model string, contents []GeminiChatContent) bool {
	settings := model_setting.GetGeminiSettings()
	if !settings.EnableCache {
		return false
	}

	tokenCount := CountTokensFromParts(contents)
	if tokenCount < GeminiCacheMinTokenThreshold {
		common.SysLog(fmt.Sprintf("Skipping cache creation: token count %d < %d", tokenCount, GeminiCacheMinTokenThreshold))
		return false
	}
	return true
}

func GetOrCreateGeminiCache(apiKey string, channelID int, model string, request *GeminiChatRequest) (string, error) {
	if !ShouldEnableGeminiCache(model, request.Contents) {
		return "", nil
	}

	prompt := ExtractLastUserPromptText(request)
	hash := common.GetMD5Hash(model + "|" + prompt)
	redisKey := fmt.Sprintf("gemini_cache:%s:%s", channelID, hash)

	var cachedID string
	var err error

	if common.RedisEnabled {
		cachedID, err = common.RDB.Get(context.Background(), redisKey).Result()
		if err == nil && cachedID != "" {
			common.SysLog("Found cachedID in Redis: " + cachedID)

			if exists, err := LookupGeminiCacheByID(apiKey, cachedID); err == nil && exists {
				common.SysLog("Gemini cache confirmed via lookup: " + cachedID)
				return cachedID, nil
			}
			common.SysLog("Gemini lookup failed, creating new cache...")
		}
	} else {
		common.SysLog("Redis not enabled...")
	}

	newID, err := CreateGeminiCache(apiKey, model, request, hash)
	if err != nil {
		return "", err
	}

	if common.RedisEnabled {
		_ = common.RDB.Set(context.Background(), redisKey, newID, time.Hour).Err()
		common.SysLog("Gemini cache saved to Redis: " + redisKey + " = " + newID)
	}

	return newID, nil
}

func LookupGeminiCacheByID(apiKey string, cachedID string) (bool, error) {
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/%s?key=%s", cachedID, apiKey)

	resp, err := service.GetHttpClient().Get(url)
	if err != nil {
		return false, fmt.Errorf("lookup by ID failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return true, nil
	}

	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}

	var errResp map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&errResp)
	return false, fmt.Errorf("lookup by ID failed: %v", errResp)
}

func CreateGeminiCache(apiKey, model string, request *GeminiChatRequest, displayName string) (string, error) {
	if !strings.HasPrefix(model, "models/") {
		model = "models/" + model
	}

	cacheReq := &GeminiCachedContentRequest{
		Model:             model,
		SystemInstruction: request.SystemInstructions,
		Contents:          []GeminiChatContent{request.Contents[len(request.Contents)-1]},
		Ttl:               "3600s",
		DisplayName:       displayName,
	}

	body, err := json.Marshal(cacheReq)
	if err != nil {
		return "", fmt.Errorf("failed to marshal cache request: %w", err)
	}

	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/cachedContents?key=%s", apiKey)
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(string(body)))
	if err != nil {
		return "", fmt.Errorf("failed to create HTTP request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := service.GetHttpClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to perform HTTP request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errResp map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&errResp)
		return "", fmt.Errorf("cache creation failed: %v", errResp)
	}

	var cacheResp GeminiCachedContentResponse
	if err := json.NewDecoder(resp.Body).Decode(&cacheResp); err != nil {
		return "", fmt.Errorf("error decoding cache response: %w", err)
	}

	common.SysLog("Cache created: " + cacheResp.Name)
	return cacheResp.Name, nil
}


func CountTokensFromParts(contents []GeminiChatContent) int {
	count := 0
	for _, content := range contents {
		for _, part := range content.Parts {
			if part.Text != "" {
				count += len(strings.Split(part.Text, " "))
			}
		}
	}
	return count
}

func ExtractLastUserPromptText(request *GeminiChatRequest) string {
	for i := len(request.Contents) - 1; i >= 0; i-- {
		content := request.Contents[i]
		if content.Role == "user" {
			var b strings.Builder
			for _, part := range content.Parts {
				if part.Text != "" {
					b.WriteString(part.Text)
					//appendPartAsString(&b, part)
				}
				if part.InlineData != nil {
					b.WriteString(part.InlineData.MimeType)
					b.WriteString(":")
					b.WriteString(part.InlineData.Data)
				}
				if part.FileData != nil {
					b.WriteString(part.FileData.MimeType)
					b.WriteString(":")
					b.WriteString(part.FileData.FileUri)
				}
			}
			return b.String()
		}
	}
	return ""
}
