package main

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

func parseID(value string) (int64, error) {
	return strconv.ParseInt(value, 10, 64)
}

func queryInt(value string, fallback int) int {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func randomToken(bytes int) (string, error) {
	data := make([]byte, bytes)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return hex.EncodeToString(data), nil
}

func nullableInt64(value int64) any {
	if value == 0 {
		return nil
	}
	return value
}

func ok(c *gin.Context, body any) {
	c.JSON(http.StatusOK, body)
}

func fail(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{"error": message})
}
