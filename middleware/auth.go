package middleware

import (
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

type LINEClaims struct {
	Sub     string `json:"sub"`
	Name    string `json:"name"`
	Picture string `json:"picture"`
	Email   string `json:"email"`
	jwt.RegisteredClaims
}

const lineUserIDKey = "lineUserID"
const lineClaimsKey = "lineClaims"

func JWTAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "authorization header required"})
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid authorization header format"})
			return
		}

		tokenStr := parts[1]
		channelSecret := os.Getenv("LINE_CHANNEL_SECRET")

		claims := &LINEClaims{}
		token, err := jwt.ParseWithClaims(tokenStr, claims, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, jwt.ErrSignatureInvalid
			}
			return []byte(channelSecret), nil
		})

		if err != nil || !token.Valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
			return
		}

		if claims.Sub == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "line user id (sub) missing from token"})
			return
		}

		c.Set(lineUserIDKey, claims.Sub)
		c.Set(lineClaimsKey, claims)
		c.Next()
	}
}

func GetLineUserID(c *gin.Context) string {
	val, _ := c.Get(lineUserIDKey)
	id, _ := val.(string)
	return id
}

func GetLineClaims(c *gin.Context) *LINEClaims {
	val, _ := c.Get(lineClaimsKey)
	claims, _ := val.(*LINEClaims)
	return claims
}
