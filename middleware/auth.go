package middleware

import (
"net/http"
"os"
"strings"

"github.com/gin-gonic/gin"
"github.com/golang-jwt/jwt/v5"
)

func JWTAuth() gin.HandlerFunc {
return func(c *gin.Context) {
authHeader := c.GetHeader("Authorization")
if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing token"})
return
}

tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
secret := []byte(os.Getenv("LINE_CHANNEL_SECRET"))

token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
return nil, jwt.ErrSignatureInvalid
}
return secret, nil
})

if err != nil || !token.Valid {
c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
return
}

claims, ok := token.Claims.(jwt.MapClaims)
if !ok {
c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid claims"})
return
}

c.Set("line_id", claims["sub"])
c.Set("name", claims["name"])
c.Next()
}
}

func GetLineUserID(c *gin.Context) string {
id, _ := c.Get("line_id")
if str, ok := id.(string); ok {
return str
}
return ""
}
