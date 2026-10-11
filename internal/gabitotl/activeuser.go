package gabitotl

import (
	"os"
	"os/user"
	"strings"
)

// EnvUser overrides the identity derived from the OS user.
const EnvUser = "GABITOTL_USER"

// ActiveUserID derives the id of the person running GabitoTL: GABITOTL_USER when
// set, else the OS user name. The result is a slug; it is empty when nothing
// usable is available. It lives here (and package identity calls it) because the
// per-person memory settings need the id and identity imports this package.
func ActiveUserID() string {
	if v := strings.TrimSpace(os.Getenv(EnvUser)); v != "" {
		return Slug(v)
	}
	raw := ""
	if u, err := user.Current(); err == nil {
		raw = u.Username
	}
	if raw == "" {
		for _, k := range []string{"USER", "USERNAME", "LOGNAME"} {
			if raw = os.Getenv(k); raw != "" {
				break
			}
		}
	}
	return SlugOSName(raw)
}

// SlugOSName strips a Windows DOMAIN\ prefix and a user@domain suffix, then
// slugs what is left.
func SlugOSName(raw string) string {
	raw = strings.TrimSpace(raw)
	if i := strings.LastIndexByte(raw, '\\'); i >= 0 {
		raw = raw[i+1:]
	}
	if i := strings.IndexByte(raw, '@'); i >= 0 {
		raw = raw[:i]
	}
	return Slug(raw)
}
