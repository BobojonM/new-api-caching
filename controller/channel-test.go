package controller

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"one-api/common"
	"one-api/constant"
	"one-api/dto"
	"one-api/middleware"
	"one-api/model"
	"one-api/relay"
	relaycommon "one-api/relay/common"
	relayconstant "one-api/relay/constant"
	"one-api/relay/helper"
	"one-api/service"
	"one-api/types"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
)

type testResult struct {
	context     *gin.Context
	localErr    error
	newAPIError *types.NewAPIError
}

func testChannel(channel *model.Channel, testModel string, testType string) testResult {
	tik := time.Now()
	if channel.Type == constant.ChannelTypeMidjourney {
		return testResult{localErr: errors.New("midjourney channel test is not supported")}
	}
	if channel.Type == constant.ChannelTypeMidjourneyPlus {
		return testResult{localErr: errors.New("midjourney plus channel test is not supported")}
	}
	if channel.Type == constant.ChannelTypeSunoAPI {
		return testResult{localErr: errors.New("suno channel test is not supported")}
	}
	if channel.Type == constant.ChannelTypeKling {
		return testResult{localErr: errors.New("kling channel test is not supported")}
	}
	if channel.Type == constant.ChannelTypeJimeng {
		return testResult{localErr: errors.New("jimeng channel test is not supported")}
	}
	if channel.Type == constant.ChannelTypeVidu {
		return testResult{localErr: errors.New("vidu channel test is not supported")}
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	requestPath := "/v1/chat/completions"
	isEmbeddingModel := func(m string) bool {
		lm := strings.ToLower(m)
		return strings.Contains(lm, "embedding") ||
			strings.HasPrefix(m, "m3e") ||
			strings.Contains(m, "bge-") ||
			strings.Contains(lm, "embed")
	}
	if isEmbeddingModel(testModel) || channel.Type == constant.ChannelTypeMokaAI {
		requestPath = "/v1/embeddings"
	}

	c.Request = &http.Request{
		Method: "POST",
		URL:    &url.URL{Path: requestPath},
		Body:   nil,
		Header: make(http.Header),
	}

	if testModel == "" {
		if channel.TestModel != nil && *channel.TestModel != "" {
			testModel = *channel.TestModel
		} else {
			if len(channel.GetModels()) > 0 {
				testModel = channel.GetModels()[0]
			} else {
				testModel = "gpt-4o-mini"
			}
		}
	}
	testType = strings.ToLower(strings.TrimSpace(testType))
	if testType == "" {
		testType = "text"
	}

	// user cache
	cache, err := model.GetUserCache(1)
	if err != nil {
		return testResult{localErr: err}
	}
	cache.WriteContext(c)

	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("channel", channel.Type)
	c.Set("base_url", channel.GetBaseURL())
	group, _ := model.GetUserGroup(1, false)
	c.Set("group", group)

	newAPIError := middleware.SetupContextForSelectedChannel(c, channel, testModel)
	if newAPIError != nil {
		return testResult{context: c, localErr: newAPIError, newAPIError: newAPIError}
	}

	info := relaycommon.GenRelayInfo(c)

	if err := helper.ModelMappedHelper(c, info, nil); err != nil {
		return testResult{
			context:     c,
			localErr:    err,
			newAPIError: types.NewError(err, types.ErrorCodeChannelModelMappedError),
		}
	}
	testModel = info.UpstreamModelName

	apiType, _ := common.ChannelType2APIType(channel.Type)
	adaptor := relay.GetAdaptor(apiType)
	if adaptor == nil {
		err := fmt.Errorf("invalid api type: %d, adaptor is nil", apiType)
		return testResult{context: c, localErr: err, newAPIError: types.NewError(err, types.ErrorCodeInvalidApiType)}
	}

	request := buildTestRequest(testModel, testType)

	logInfo := *info
	logInfo.ApiKey = ""
	common.SysLog(fmt.Sprintf("testing channel %d with model %s (type=%s), info %+v", channel.Id, testModel, testType, logInfo))

	priceData, err := helper.ModelPriceHelper(c, info, 0, int(request.GetMaxTokens()))
	if err != nil {
		return testResult{context: c, localErr: err, newAPIError: types.NewError(err, types.ErrorCodeModelPriceError)}
	}

	adaptor.Init(info)

	var convertedRequest any
	if info.RelayMode == relayconstant.RelayModeEmbeddings {
		embeddingRequest := dto.EmbeddingRequest{
			Input: request.Input,
			Model: request.Model,
		}
		convertedRequest, err = adaptor.ConvertEmbeddingRequest(c, info, embeddingRequest)
	} else {
		convertedRequest, err = adaptor.ConvertOpenAIRequest(c, info, request)
	}
	if err != nil {
		return testResult{context: c, localErr: err, newAPIError: types.NewError(err, types.ErrorCodeConvertRequestFailed)}
	}

	jsonData, err := json.Marshal(convertedRequest)
	if err != nil {
		return testResult{context: c, localErr: err, newAPIError: types.NewError(err, types.ErrorCodeJsonMarshalFailed)}
	}

	requestBody := bytes.NewBuffer(jsonData)
	c.Request.Body = io.NopCloser(requestBody)

	resp, err := adaptor.DoRequest(c, info, requestBody)
	if err != nil {
		return testResult{context: c, localErr: err, newAPIError: types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)}
	}

	var httpResp *http.Response
	if resp != nil {
		httpResp = resp.(*http.Response)
		if httpResp.StatusCode != http.StatusOK {
			err := service.RelayErrorHandler(httpResp, true)
			return testResult{context: c, localErr: err, newAPIError: types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)}
		}
	}

	usageA, respErr := adaptor.DoResponse(c, httpResp, info)
	if respErr != nil {
		return testResult{context: c, localErr: respErr, newAPIError: respErr}
	}
	if usageA == nil {
		err := errors.New("usage is nil")
		return testResult{context: c, localErr: err, newAPIError: types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)}
	}
	usage := usageA.(*dto.Usage)

	result := w.Result()
	respBody, err := io.ReadAll(result.Body)
	if err != nil {
		return testResult{context: c, localErr: err, newAPIError: types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)}
	}
	info.PromptTokens = usage.PromptTokens

	quota := 0
	if !priceData.UsePrice {
		quota = usage.PromptTokens + int(math.Round(float64(usage.CompletionTokens)*priceData.CompletionRatio))
		quota = int(math.Round(float64(quota) * priceData.ModelRatio))
		if priceData.ModelRatio != 0 && quota <= 0 {
			quota = 1
		}
	} else {
		quota = int(priceData.ModelPrice * common.QuotaPerUnit)
	}

	tok := time.Now()
	milliseconds := tok.Sub(tik).Milliseconds()
	consumedTime := float64(milliseconds) / 1000.0

	other := service.GenerateTextOtherInfo(
		c, info,
		priceData.ModelRatio, priceData.GroupRatioInfo.GroupRatio, priceData.CompletionRatio,
		usage.PromptTokensDetails.CachedTokens, priceData.CacheRatio, priceData.ModelPrice, priceData.GroupRatioInfo.GroupSpecialRatio,
	)
	model.RecordConsumeLog(c, 1, model.RecordConsumeLogParams{
		ChannelId:        channel.Id,
		PromptTokens:     usage.PromptTokens,
		CompletionTokens: usage.CompletionTokens,
		ModelName:        info.OriginModelName,
		TokenName:        "模型测试",
		Quota:            quota,
		Content:          "模型测试",
		UseTimeSeconds:   int(consumedTime),
		IsStream:         info.IsStream,
		Group:            info.UsingGroup,
		Other:            other,
	})

	common.SysLog(fmt.Sprintf("testing channel #%d, response: \n%s", channel.Id, string(respBody)))

	return testResult{context: c, localErr: nil, newAPIError: nil}
}

func buildTestRequest(modelName string, testType string) *dto.GeneralOpenAIRequest {
	req := &dto.GeneralOpenAIRequest{
		Model:  "",
		Stream: false,
	}

	isEmbedding := func(m string) bool {
		lm := strings.ToLower(m)
		return strings.Contains(lm, "embedding") ||
			strings.HasPrefix(m, "m3e") ||
			strings.Contains(m, "bge-") ||
			strings.Contains(lm, "embed")
	}

	if isEmbedding(modelName) {
		req.Model = modelName
		// dto.GeneralOpenAIRequest.Input — any
		req.Input = []any{"hello world"}
		return req
	}

	if strings.HasPrefix(modelName, "o") {
		req.MaxCompletionTokens = 32
	} else if strings.Contains(modelName, "thinking") {
		if !strings.Contains(modelName, "claude") {
			req.MaxTokens = 64
		}
	} else if strings.Contains(modelName, "gemini") {
		req.MaxTokens = 128
	} else {
		req.MaxTokens = 64
	}
	temp := 0.0
	req.Temperature = &temp
	req.Model = modelName

	switch strings.ToLower(testType) {
	case "json":
		sys := dto.Message{
			Role:    req.GetSystemRoleName(),
			Content: "Return ONLY a valid JSON object. No prose, no code fences, no explanations.",
		}
		user := dto.Message{
			Role: "user",
			Content: "Return a minimal JSON with fields: {\"ok\": true, \"model\": \"" + modelName + "\", \"ts\": current unix timestamp integer}.",
		}
		req.Messages = append(req.Messages, sys, user)
		req.ResponseFormat = &dto.ResponseFormat{
			Type: "json_object",
		}

	case "function":
		req.Tools = []dto.ToolCallRequest{
			{
				Type: "function",
				Function: dto.FunctionRequest{
					Name:        "add",
					Description: "Sum two integers",
					Parameters: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"a": map[string]any{"type": "integer"},
							"b": map[string]any{"type": "integer"},
						},
						"required":             []string{"a", "b"},
						"additionalProperties": false,
					},
				},
			},
		}
		// Форсим вызов инструмента
		req.ToolChoice = "required"

		sys := dto.Message{
			Role:    req.GetSystemRoleName(),
			Content: "You are a function-calling assistant. Prefer calling tools when available.",
		}
		user := dto.Message{
			Role:    "user",
			Content: "Please add 2 and 3.",
		}
		req.Messages = append(req.Messages, sys, user)

	default: // "text"
		msg := dto.Message{
			Role:    "user",
			Content: "hi",
		}
		req.Messages = append(req.Messages, msg)
	}

	return req
}

func TestChannel(c *gin.Context) {
	channelId, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	channel, err := model.CacheGetChannel(channelId)
	if err != nil {
		channel, err = model.GetChannelById(channelId, true)
		if err != nil {
			common.ApiError(c, err)
			return
		}
	}

	testModel := c.Query("model")
	testType := strings.ToLower(c.Query("type")) // "", "text", "json", "function"
	tik := time.Now()

	result := testChannel(channel, testModel, testType)
	if result.localErr != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": result.localErr.Error(),
			"time":    0.0,
		})
		return
	}

	tok := time.Now()
	milliseconds := tok.Sub(tik).Milliseconds()
	go channel.UpdateResponseTime(milliseconds)
	consumedTime := float64(milliseconds) / 1000.0
	if result.newAPIError != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": result.newAPIError.Error(),
			"time":    consumedTime,
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"time":    consumedTime,
	})
}

var testAllChannelsLock sync.Mutex
var testAllChannelsRunning bool = false

func testAllChannels(notify bool) error {
	testAllChannelsLock.Lock()
	if testAllChannelsRunning {
		testAllChannelsLock.Unlock()
		return errors.New("测试已在运行中")
	}
	testAllChannelsRunning = true
	testAllChannelsLock.Unlock()

	channels, getChannelErr := model.GetAllChannels(0, 0, true, false)
	if getChannelErr != nil {
		return getChannelErr
	}
	var disableThreshold = int64(common.ChannelDisableThreshold * 1000)
	if disableThreshold == 0 {
		disableThreshold = 10000000
	}

	gopool.Go(func() {
		defer func() {
			testAllChannelsLock.Lock()
			testAllChannelsRunning = false
			testAllChannelsLock.Unlock()
		}()

		for _, channel := range channels {
			isChannelEnabled := channel.Status == common.ChannelStatusEnabled
			tik := time.Now()
			result := testChannel(channel, "", "")
			tok := time.Now()
			milliseconds := tok.Sub(tik).Milliseconds()

			shouldBanChannel := false
			newAPIError := result.newAPIError
			if newAPIError != nil {
				shouldBanChannel = service.ShouldDisableChannel(channel.Type, result.newAPIError)
			}

			if common.AutomaticDisableChannelEnabled && !shouldBanChannel {
				if milliseconds > disableThreshold {
					err := fmt.Errorf("响应时间 %.2fs 超过阈值 %.2fs", float64(milliseconds)/1000.0, float64(disableThreshold)/1000.0)
					newAPIError = types.NewOpenAIError(err, types.ErrorCodeChannelResponseTimeExceeded, http.StatusRequestTimeout)
					shouldBanChannel = true
				}
			}

			// disable
			if isChannelEnabled && shouldBanChannel && channel.GetAutoBan() {
				go processChannelError(
					result.context,
					*types.NewChannelError(
						channel.Id,
						channel.Type,
						channel.Name,
						channel.ChannelInfo.IsMultiKey,
						common.GetContextKeyString(result.context, constant.ContextKeyChannelKey),
						channel.GetAutoBan(),
					),
					newAPIError,
				)
			}

			// enable
			if !isChannelEnabled && service.ShouldEnableChannel(newAPIError, channel.Status) {
				service.EnableChannel(channel.Id, common.GetContextKeyString(result.context, constant.ContextKeyChannelKey), channel.Name)
			}

			channel.UpdateResponseTime(milliseconds)
			time.Sleep(common.RequestInterval)
		}

		if notify {
			service.NotifyRootUser(dto.NotifyTypeChannelTest, "通道测试完成", "所有通道测试已完成")
		}
	})
	return nil
}

func TestAllChannels(c *gin.Context) {
	err := testAllChannels(true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

func AutomaticallyTestChannels(frequency int) {
	if frequency <= 0 {
		common.SysLog("CHANNEL_TEST_FREQUENCY is not set or invalid, skipping automatic channel test")
		return
	}
	for {
		time.Sleep(time.Duration(frequency) * time.Minute)
		common.SysLog("testing all channels")
		_ = testAllChannels(false)
		common.SysLog("channel test finished")
	}
}
