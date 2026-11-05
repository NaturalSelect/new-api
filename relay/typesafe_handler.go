package relay

import (
	"bytes"
	"fmt"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel/typesafe"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

func TypeSafeHelper(c *gin.Context, info *relaycommon.RelayInfo) (newAPIError *types.NewAPIError) {
	info.InitChannelMeta(c)
	typesafe.ClearUpstreamError(c)

	request, ok := info.Request.(*dto.TypeSafeRequest)
	if !ok {
		return types.NewErrorWithStatusCode(fmt.Errorf("invalid request type, expected dto.TypeSafeRequest, got %T", info.Request), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}

	// NOTE: The request is mapped in place instead of deep-copied, because a JSON round trip would lose the original bytes and member order.
	if err := helper.ModelMappedHelper(c, info, request); err != nil {
		return types.NewError(err, types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry())
	}

	adaptor := GetAdaptor(info.ApiType)
	if adaptor == nil {
		return types.NewError(fmt.Errorf("invalid api type: %d", info.ApiType), types.ErrorCodeInvalidApiType, types.ErrOptionWithSkipRetry())
	}
	adaptor.Init(info)

	requestBody, closer, err := typeSafeRequestBody(c, info, request)
	if err != nil {
		return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	if closer != nil {
		defer closer.Close()
	}

	resp, err := adaptor.DoRequest(c, info, requestBody)
	if err != nil {
		return types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	}

	statusCodeMappingStr := c.GetString("status_code_mapping")
	var httpResp *http.Response
	if resp != nil {
		httpResp = resp.(*http.Response)
		if httpResp.StatusCode != http.StatusOK {
			newAPIError = typesafe.ErrorFromResponse(c, httpResp)
			service.ResetStatusCode(newAPIError, statusCodeMappingStr)
			return newAPIError
		}
	}

	usage, newAPIError := adaptor.DoResponse(c, httpResp, info)
	if newAPIError != nil {
		service.ResetStatusCode(newAPIError, statusCodeMappingStr)
		return newAPIError
	}
	service.PostTextConsumeQuota(c, info, usage.(*dto.Usage), nil)
	return nil
}

// typeSafeRequestBody builds the upstream body from the client's bytes. The
// original storage is forwarded as is unless the model was redirected, in
// which case only the model value (or the multipart request part) changes.
func typeSafeRequestBody(c *gin.Context, info *relaycommon.RelayInfo, request *dto.TypeSafeRequest) (io.Reader, io.Closer, error) {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, nil, err
	}

	original, _, err := dto.ReadTypeSafeRequestJSON(c)
	if err != nil {
		return nil, nil, err
	}
	if bytes.Equal(original, request.RequestJSON) {
		info.UpstreamRequestBodySize = storage.Size()
		return common.ReaderOnly(storage), nil, nil
	}

	if !request.Multipart {
		return outboundBody(info, request.RequestJSON)
	}
	raw, err := storage.Bytes()
	if err != nil {
		return nil, nil, err
	}
	rewritten, err := typesafe.RewriteRequestPart(raw, c.Request.Header.Get("Content-Type"), request.RequestJSON)
	if err != nil {
		return nil, nil, err
	}
	return outboundBody(info, rewritten)
}

func outboundBody(info *relaycommon.RelayInfo, data []byte) (io.Reader, io.Closer, error) {
	body, size, closer, err := relaycommon.NewOutboundJSONBody(data)
	if err != nil {
		return nil, nil, err
	}
	info.UpstreamRequestBodySize = size
	return body, closer, nil
}
