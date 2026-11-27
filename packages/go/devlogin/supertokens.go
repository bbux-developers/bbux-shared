package devlogin

import (
	"context"
	"fmt"
	"net/http"

	"github.com/supertokens/supertokens-golang/recipe/passwordless"
	"github.com/supertokens/supertokens-golang/recipe/session"
)

// LinkFunc ties the app's user row to a SuperTokens user id (bbux: linkUser).
type LinkFunc func(ctx context.Context, email, stUserID string) error

// SuperTokensSignIn is a SignInFunc for apps on SuperTokens passwordless
// (the shared packages/go/auth module). It signs the email up/in on the
// "public" tenant, calls link (the recipe-level call does NOT run the API
// override, so the app's OnConsume hook doesn't fire — link stands in for it),
// then creates a real session on the response. link may be nil.
func SuperTokensSignIn(link LinkFunc) SignInFunc {
	return func(w http.ResponseWriter, r *http.Request, email string) error {
		res, err := passwordless.SignInUpByEmail("public", email)
		if err != nil {
			return fmt.Errorf("supertokens signInUp: %w", err)
		}
		if link != nil {
			if err := link(r.Context(), email, res.User.ID); err != nil {
				return fmt.Errorf("link user: %w", err)
			}
		}
		if _, err := session.CreateNewSession(r, w, "public", res.User.ID, nil, nil); err != nil {
			return fmt.Errorf("create session: %w", err)
		}
		return nil
	}
}
