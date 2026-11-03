package auth

import (
	_ "embed"
	"strings"
	"sync"
)

// commonPasswords holds one lowercase password for each line, taken from lists of the passwords that leaked most often
//
//go:embed common-passwords.txt
var commonPasswords string

var (
	commonOnce sync.Once
	commonSet  map[string]struct{}
)

// isCommonPassword reports whether the password is on the list, whatever its letter case
func isCommonPassword(p string) bool {
	commonOnce.Do(func() {
		lines := strings.Fields(commonPasswords)
		commonSet = make(map[string]struct{}, len(lines))

		for _, line := range lines {
			commonSet[line] = struct{}{}
		}
	})

	_, found := commonSet[strings.ToLower(p)]

	return found
}
