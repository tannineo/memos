package v1

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"

	"github.com/labstack/echo/v4"
	"github.com/pkg/errors"
	"github.com/usememos/memos/common/util"
	"github.com/usememos/memos/plugin/idp"
	"github.com/usememos/memos/plugin/idp/oauth2"
	"github.com/usememos/memos/store"
	"golang.org/x/crypto/bcrypt"
)

type SignIn struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type SSOSignIn struct {
	IdentityProviderID int32  `json:"identityProviderId"`
	Code               string `json:"code"`
	RedirectURI        string `json:"redirectUri"`
}

type SignUp struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *APIV1Service) registerAuthRoutes(g *echo.Group) {
	// POST /auth/signin - Sign in.
	g.POST("/auth/signin", func(c echo.Context) error {
		ctx := c.Request().Context()
		signin := &SignIn{}

		disablePasswordLoginSystemSetting, err := s.Store.GetSystemSetting(ctx, &store.FindSystemSetting{
			Name: SystemSettingDisablePasswordLoginName.String(),
		})
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to find system setting").SetInternal(err)
		}
		if disablePasswordLoginSystemSetting != nil {
			disablePasswordLogin := false
			err = json.Unmarshal([]byte(disablePasswordLoginSystemSetting.Value), &disablePasswordLogin)
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "Failed to unmarshal system setting").SetInternal(err)
			}
			if disablePasswordLogin {
				return echo.NewHTTPError(http.StatusUnauthorized, "Password login is deactivated")
			}
		}

		if err := json.NewDecoder(c.Request().Body).Decode(signin); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "Malformatted signin request").SetInternal(err)
		}

		user, err := s.Store.GetUser(ctx, &store.FindUser{
			Username: &signin.Username,
		})
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Incorrect login credentials, please try again")
		}
		if user == nil {
			return echo.NewHTTPError(http.StatusUnauthorized, "Incorrect login credentials, please try again")
		} else if user.RowStatus == store.Archived {
			return echo.NewHTTPError(http.StatusForbidden, fmt.Sprintf("User has been archived with username %s", signin.Username))
		}

		// Compare the stored hashed password, with the hashed version of the password that was received.
		if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(signin.Password)); err != nil {
			// If the two passwords don't match, return a 401 status.
			return echo.NewHTTPError(http.StatusUnauthorized, "Incorrect login credentials, please try again")
		}

		if err := GenerateTokensAndSetCookies(c, user, s.Secret); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to generate tokens").SetInternal(err)
		}
		if err := s.createAuthSignInActivity(c, user); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to create activity").SetInternal(err)
		}
		userMessage := convertUserFromStore(user)
		return c.JSON(http.StatusOK, userMessage)
	})

	// POST /auth/signin/sso - Sign in with SSO
	g.POST("/auth/signin/sso", func(c echo.Context) error {
		ctx := c.Request().Context()
		signin := &SSOSignIn{}
		if err := json.NewDecoder(c.Request().Body).Decode(signin); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "Malformatted signin request").SetInternal(err)
		}

		identityProvider, err := s.Store.GetIdentityProvider(ctx, &store.FindIdentityProvider{
			ID: &signin.IdentityProviderID,
		})
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to find identity provider").SetInternal(err)
		}
		if identityProvider == nil {
			return echo.NewHTTPError(http.StatusNotFound, "Identity provider not found")
		}

		var userInfo *idp.IdentityProviderUserInfo
		if identityProvider.Type == store.IdentityProviderOAuth2Type {
			oauth2IdentityProvider, err := oauth2.NewIdentityProvider(identityProvider.Config.OAuth2Config)
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "Failed to create identity provider instance").SetInternal(err)
			}
			token, err := oauth2IdentityProvider.ExchangeToken(ctx, signin.RedirectURI, signin.Code)
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "Failed to exchange token").SetInternal(err)
			}
			userInfo, err = oauth2IdentityProvider.UserInfo(token)
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get user info").SetInternal(err)
			}
		}

		identifierFilter := identityProvider.IdentifierFilter
		if identifierFilter != "" {
			identifierFilterRegex, err := regexp.Compile(identifierFilter)
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "Failed to compile identifier filter").SetInternal(err)
			}
			if !identifierFilterRegex.MatchString(userInfo.Identifier) {
				return echo.NewHTTPError(http.StatusUnauthorized, "Access denied, identifier does not match the filter.").SetInternal(err)
			}
		}

		user, err := s.Store.GetUser(ctx, &store.FindUser{
			Username: &userInfo.Identifier,
		})
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Incorrect login credentials, please try again")
		}
		if user == nil {
			userCreate := &store.User{
				Username: userInfo.Identifier,
				// The new signup user should be normal user by default.
				Role:     store.RoleUser,
				Nickname: userInfo.DisplayName,
				Email:    userInfo.Email,
				OpenID:   util.GenUUID(),
			}
			password, err := util.RandomString(20)
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "Failed to generate random password").SetInternal(err)
			}
			passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "Failed to generate password hash").SetInternal(err)
			}
			userCreate.PasswordHash = string(passwordHash)
			user, err = s.Store.CreateUser(ctx, userCreate)
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "Failed to create user").SetInternal(err)
			}
		}
		if user.RowStatus == store.Archived {
			return echo.NewHTTPError(http.StatusForbidden, fmt.Sprintf("User has been archived with username %s", userInfo.Identifier))
		}

		if err := GenerateTokensAndSetCookies(c, user, s.Secret); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to generate tokens").SetInternal(err)
		}
		if err := s.createAuthSignInActivity(c, user); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to create activity").SetInternal(err)
		}
		userMessage := convertUserFromStore(user)
		return c.JSON(http.StatusOK, userMessage)
	})

	// POST /auth/signup - Sign up a new user.
	g.POST("/auth/signup", func(c echo.Context) error {
		ctx := c.Request().Context()
		signup := &SignUp{}
		if err := json.NewDecoder(c.Request().Body).Decode(signup); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "Malformatted signup request").SetInternal(err)
		}

		hostUserType := store.RoleHost
		existedHostUsers, err := s.Store.ListUsers(ctx, &store.FindUser{
			Role: &hostUserType,
		})
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "Failed to find users").SetInternal(err)
		}

		userCreate := &store.User{
			Username: signup.Username,
			// The new signup user should be normal user by default.
			Role:     store.RoleUser,
			Nickname: signup.Username,
			OpenID:   util.GenUUID(),
		}
		if len(existedHostUsers) == 0 {
			// Change the default role to host if there is no host user.
			userCreate.Role = store.RoleHost
		} else {
			allowSignUpSetting, err := s.Store.GetSystemSetting(ctx, &store.FindSystemSetting{
				Name: SystemSettingAllowSignUpName.String(),
			})
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "Failed to find system setting").SetInternal(err)
			}

			allowSignUpSettingValue := false
			if allowSignUpSetting != nil {
				err = json.Unmarshal([]byte(allowSignUpSetting.Value), &allowSignUpSettingValue)
				if err != nil {
					return echo.NewHTTPError(http.StatusInternalServerError, "Failed to unmarshal system setting allow signup").SetInternal(err)
				}
			}
			if !allowSignUpSettingValue {
				return echo.NewHTTPError(http.StatusUnauthorized, "signup is disabled").SetInternal(err)
			}
		}

		passwordHash, err := bcrypt.GenerateFromPassword([]byte(signup.Password), bcrypt.DefaultCost)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to generate password hash").SetInternal(err)
		}

		userCreate.PasswordHash = string(passwordHash)
		user, err := s.Store.CreateUser(ctx, userCreate)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to create user").SetInternal(err)
		}
		if err := GenerateTokensAndSetCookies(c, user, s.Secret); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to generate tokens").SetInternal(err)
		}
		if err := s.createAuthSignUpActivity(c, user); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to create activity").SetInternal(err)
		}

		userMessage := convertUserFromStore(user)
		return c.JSON(http.StatusOK, userMessage)
	})

	// POST /auth/signout - Sign out.
	g.POST("/auth/signout", func(c echo.Context) error {
		RemoveTokensAndCookies(c)
		return c.JSON(http.StatusOK, true)
	})
}

func (s *APIV1Service) createAuthSignInActivity(c echo.Context, user *store.User) error {
	ctx := c.Request().Context()
	payload := ActivityUserAuthSignInPayload{
		UserID: user.ID,
		IP:     echo.ExtractIPFromRealIPHeader()(c.Request()),
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return errors.Wrap(err, "failed to marshal activity payload")
	}
	activity, err := s.Store.CreateActivity(ctx, &store.Activity{
		CreatorID: user.ID,
		Type:      string(ActivityUserAuthSignIn),
		Level:     string(ActivityInfo),
		Payload:   string(payloadBytes),
	})
	if err != nil || activity == nil {
		return errors.Wrap(err, "failed to create activity")
	}
	return err
}

func (s *APIV1Service) createAuthSignUpActivity(c echo.Context, user *store.User) error {
	ctx := c.Request().Context()
	payload := ActivityUserAuthSignUpPayload{
		Username: user.Username,
		IP:       echo.ExtractIPFromRealIPHeader()(c.Request()),
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return errors.Wrap(err, "failed to marshal activity payload")
	}
	activity, err := s.Store.CreateActivity(ctx, &store.Activity{
		CreatorID: user.ID,
		Type:      string(ActivityUserAuthSignUp),
		Level:     string(ActivityInfo),
		Payload:   string(payloadBytes),
	})
	if err != nil || activity == nil {
		return errors.Wrap(err, "failed to create activity")
	}
	return err
}
