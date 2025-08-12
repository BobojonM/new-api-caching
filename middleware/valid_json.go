package middleware

import (
    "bytes"
    "encoding/json"
    "io"
    "net/http"

    "github.com/gin-gonic/gin"
)

func ValidateJSONMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        // Checking POST, PUT, PATCH
        if c.Request.Method != http.MethodPost &&
            c.Request.Method != http.MethodPut &&
            c.Request.Method != http.MethodPatch {
            c.Next()
            return
        }

        // Reading body
        body, err := io.ReadAll(c.Request.Body)
        if err != nil {
            c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to read request body"})
            c.Abort()
            return
        }

        c.Request.Body = io.NopCloser(bytes.NewBuffer(body))

        // Empty body — skipping
        if len(body) == 0 {
            c.Next()
            return
        }

        // Check Valid JSON
        if !json.Valid(body) {
            c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
            c.Abort()
            return
        }

        c.Next()
    }
}
