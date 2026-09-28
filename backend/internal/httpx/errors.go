// Package httpx holds the wire contract shared by every handler: the JSON
// error envelope, the JSON success helper and the ID type used in responses.
package httpx

import (
	"encoding/json"
	"log"
	"net/http"
)

// Error codes are fixed strings. Handlers select a code; the message is always
// derived from it so that responses can never leak SQL, paths or parser output.
const (
	CodeBadRequest           = "bad_request"
	CodeUnauthorized         = "unauthorized"
	CodeForbidden            = "forbidden"
	CodeNotFound             = "not_found"
	CodeMethodNotAllowed     = "method_not_allowed"
	CodePayloadTooLarge      = "payload_too_large"
	CodeUnsupportedMediaType = "unsupported_media_type"
	CodeUnprocessableEntity  = "unprocessable_entity"
	CodeServiceUnavailable   = "service_unavailable"
	CodeInternal             = "internal_error"
)

var errorMessages = map[string]string{
	CodeBadRequest:           "请求无效",
	CodeUnauthorized:         "未认证",
	CodeForbidden:            "禁止访问",
	CodeNotFound:             "资源不存在",
	CodeMethodNotAllowed:     "方法不允许",
	CodePayloadTooLarge:      "请求内容过大",
	CodeUnsupportedMediaType: "不支持的文件类型",
	CodeUnprocessableEntity:  "内容无法处理",
	CodeServiceUnavailable:   "服务暂不可用",
	CodeInternal:             "服务器内部错误",
}

// ErrorBody is the inner object of the fixed {"error":{...}} envelope.
type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ErrorResponse is the only error shape the API ever returns.
type ErrorResponse struct {
	Error ErrorBody `json:"error"`
}

// WriteJSON encodes v as the response body. Encoding happens into a buffer
// first so a failure cannot emit a half-written body after the status line.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		log.Printf("httpx: marshal response: %v", err)
		WriteError(w, http.StatusInternalServerError, CodeInternal)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)

	if _, err := w.Write(body); err != nil {
		log.Printf("httpx: write response: %v", err)
	}
}

// WriteError emits the fixed error envelope for code, using its canonical
// message. Callers cannot pass free-form text, which keeps internal detail out
// of responses by construction.
func WriteError(w http.ResponseWriter, status int, code string) {
	message, ok := errorMessages[code]
	if !ok {
		message = errorMessages[CodeInternal]
		code = CodeInternal
	}

	WriteJSON(w, status, ErrorResponse{Error: ErrorBody{Code: code, Message: message}})
}
