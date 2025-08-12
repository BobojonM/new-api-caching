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

func ShouldEnableGeminiCache(model string, tokenCount int) bool {
	settings := model_setting.GetGeminiSettings()
	if !settings.EnableCache {
		return false
	}

	if tokenCount < GeminiCacheMinTokenThreshold {
		common.SysLog(fmt.Sprintf("Skipping cache creation: token count %d < %d", tokenCount, GeminiCacheMinTokenThreshold))
		return false
	}
	return true
}

func GetOrCreateGeminiCache(apiKey string, channelID int, model string, request *GeminiChatRequest) (string, bool, int, error) {
	tokenCount := CountTokensFromParts(request.SystemInstructions)
	if !ShouldEnableGeminiCache(model, tokenCount) {
		return "", false, 0, nil
	}

	if request.SystemInstructions != nil {
		hash := HashSystemInstructions(request.SystemInstructions)
		redisKey := fmt.Sprintf("gemini_cache:%s", hash)

		var err error

		if common.RedisEnabled {
			val, err := common.RDB.Get(context.Background(), redisKey).Result()

			if err == nil && val != "" {
				var cached struct {
					CacheName string `json:"cache_name"`
					ChannelID int    `json:"channel_id"`
				}
				_ = json.Unmarshal([]byte(val), &cached)

				common.SysLog("Found cachedID in Redis: " + cached.CacheName)

				if exists, err := LookupGeminiCacheByID(apiKey, cached.CacheName); err == nil && exists {
					common.SysLog("Gemini cache confirmed via lookup: " + cached.CacheName)
					return cached.CacheName, false, 0, nil
				}
				common.SysLog("Gemini lookup failed, creating new cache...")
			}
		} else {
			common.SysLog("Redis not enabled...")
		}

		newID, err := CreateGeminiCache(apiKey, model, request, hash)
		if err != nil {
			return "", false, 0, err
		}

		if common.RedisEnabled {
			value := map[string]interface{}{
				"cache_name": newID,
				"channel_id": channelID,
			}
			jsonValue, _ := json.Marshal(value)
			_ = common.RDB.Set(context.Background(), redisKey, jsonValue, time.Hour).Err()
			common.SysLog("Gemini cache saved to Redis: " + redisKey + " = " + string(jsonValue))
		}

		return newID, true, tokenCount, nil
	}

	return "", false, 0, nil
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
		Ttl:               "600s",
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


func CountTokensFromParts(content *GeminiChatContent) int {
	count := 0
	for _, part := range content.Parts {
		if part.Text != "" {
			count += len(strings.Split(part.Text, " "))
		}
	}
	return count
}

func HashSystemInstructions(system *GeminiChatContent) string {
	if system == nil {
		return ""
	}
	bytes, _ := json.Marshal(system)
	return common.GetMD5Hash(string(bytes))
}