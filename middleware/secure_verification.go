package middleware

import (
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

const (
	// SecureVerificationSessionKey 安全验证的 session key（与 controller 保持一致）
	SecureVerificationSessionKey       = "secure_verified_at"
	secureVerificationMethodSessionKey = "secure_verified_method"
	// SecureVerificationTimeout 验证有效期（秒）
	SecureVerificationTimeout = 300 // 5分钟
	// PasswordVerificationSessionKey 登录密码验证的 session key（与 controller 保持一致）
	PasswordVerificationSessionKey = "password_verified_at"
	// NOTE: 密码验证复用 SecureVerificationTimeout（5分钟）有效期，与 2FA/Passkey 保持一致
)

// SecureVerificationRequired 安全验证中间件
// 检查用户是否在有效时间内通过了安全验证
// 如果未验证或验证已过期，返回 401 错误
// NOTE: 管理员可通过 secure_verification.require_for_channel_key 关闭该校验，此时中间件直接放行。
func SecureVerificationRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !system_setting.GetSecureVerificationSettings().RequireForChannelKey {
			c.Next()
			return
		}

		// 检查用户是否已登录
		userId := c.GetInt("id")
		if userId == 0 {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": "未登录",
			})
			c.Abort()
			return
		}

		// 检查 session 中的验证时间戳
		session := sessions.Default(c)
		verifiedAtRaw := session.Get(SecureVerificationSessionKey)

		if verifiedAtRaw == nil {
			c.JSON(http.StatusForbidden, gin.H{
				"success": false,
				"message": "需要安全验证",
				"code":    "VERIFICATION_REQUIRED",
			})
			c.Abort()
			return
		}

		verifiedAt, ok := verifiedAtRaw.(int64)
		if !ok {
			// session 数据格式错误
			clearSecureVerificationSession(session)
			c.JSON(http.StatusForbidden, gin.H{
				"success": false,
				"message": "验证状态异常，请重新验证",
				"code":    "VERIFICATION_INVALID",
			})
			c.Abort()
			return
		}

		// 检查验证是否过期
		elapsed := time.Now().Unix() - verifiedAt
		if elapsed >= SecureVerificationTimeout {
			// 验证已过期，清除 session
			clearSecureVerificationSession(session)
			c.JSON(http.StatusForbidden, gin.H{
				"success": false,
				"message": "验证已过期，请重新验证",
				"code":    "VERIFICATION_EXPIRED",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

func clearSecureVerificationSession(session sessions.Session) {
	session.Delete(SecureVerificationSessionKey)
	session.Delete(secureVerificationMethodSessionKey)
	_ = session.Save()
}

// PasswordVerificationRequired 登录密码验证中间件
// 当 secure_verification.require_password_for_channel_key 开启时，要求用户在有效时间内通过密码验证
// NOTE: 本中间件当前仅用于渠道密钥查看路由，且置于 SecureVerificationRequired 之前，
// 使密码校验先于 2FA/Passkey 校验触发（前端按 403 错误码依次弹出对应验证弹窗）。
func PasswordVerificationRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !system_setting.GetSecureVerificationSettings().RequirePasswordForChannelKey {
			c.Next()
			return
		}

		userId := c.GetInt("id")
		if userId == 0 {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": "未登录",
			})
			c.Abort()
			return
		}

		session := sessions.Default(c)
		verifiedAtRaw := session.Get(PasswordVerificationSessionKey)

		if verifiedAtRaw == nil {
			c.JSON(http.StatusForbidden, gin.H{
				"success": false,
				"message": "需要密码验证",
				"code":    "PASSWORD_VERIFICATION_REQUIRED",
			})
			c.Abort()
			return
		}

		verifiedAt, ok := verifiedAtRaw.(int64)
		if !ok {
			clearPasswordVerificationSession(session)
			c.JSON(http.StatusForbidden, gin.H{
				"success": false,
				"message": "验证状态异常，请重新验证",
				"code":    "PASSWORD_VERIFICATION_INVALID",
			})
			c.Abort()
			return
		}

		if time.Now().Unix()-verifiedAt >= SecureVerificationTimeout {
			clearPasswordVerificationSession(session)
			c.JSON(http.StatusForbidden, gin.H{
				"success": false,
				"message": "验证已过期，请重新验证",
				"code":    "PASSWORD_VERIFICATION_EXPIRED",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

func clearPasswordVerificationSession(session sessions.Session) {
	session.Delete(PasswordVerificationSessionKey)
	_ = session.Save()
}

// OptionalSecureVerification 可选的安全验证中间件
// 如果用户已验证，则在 context 中设置标记，但不阻止请求继续
// 用于某些需要区分是否已验证的场景
func OptionalSecureVerification() gin.HandlerFunc {
	return func(c *gin.Context) {
		userId := c.GetInt("id")
		if userId == 0 {
			c.Set("secure_verified", false)
			c.Next()
			return
		}

		session := sessions.Default(c)
		verifiedAtRaw := session.Get(SecureVerificationSessionKey)

		if verifiedAtRaw == nil {
			c.Set("secure_verified", false)
			c.Next()
			return
		}

		verifiedAt, ok := verifiedAtRaw.(int64)
		if !ok {
			c.Set("secure_verified", false)
			c.Next()
			return
		}

		elapsed := time.Now().Unix() - verifiedAt
		if elapsed >= SecureVerificationTimeout {
			clearSecureVerificationSession(session)
			c.Set("secure_verified", false)
			c.Next()
			return
		}

		c.Set("secure_verified", true)
		c.Set("secure_verified_at", verifiedAt)
		c.Next()
	}
}

// ClearSecureVerification 清除安全验证状态
// 用于用户登出或需要强制重新验证的场景
func ClearSecureVerification(c *gin.Context) {
	session := sessions.Default(c)
	clearSecureVerificationSession(session)
}
