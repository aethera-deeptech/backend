package auth

import "strings"

// unknownDevice is the label of a session whose browser text cannot be read
const unknownDevice = "Unknown device"

// maxLabel is the longest label that the table accepts
const maxLabel = 60

// deviceLabel makes the short text that the list of sessions shows from the User-Agent header of the sign in, such as Chrome on Windows.
// It names only the browser and the system, and it keeps no version and no address. The order of the checks matters, because Edge and
// Opera also say Chrome, and Chrome also says Safari.
func deviceLabel(userAgent string) string {
	if len(userAgent) > 512 {
		userAgent = userAgent[:512]
	}

	browser := browserOf(userAgent)
	system := systemOf(userAgent)

	switch {
	case browser != "" && system != "":
		return browser + " on " + system
	case browser != "":
		return browser
	case system != "":
		return system
	default:
		return unknownDevice
	}
}

func browserOf(ua string) string {
	switch {
	case strings.Contains(ua, "Edg/") || strings.Contains(ua, "EdgA/") || strings.Contains(ua, "EdgiOS/"):
		return "Edge"
	case strings.Contains(ua, "OPR/") || strings.Contains(ua, "Opera"):
		return "Opera"
	case strings.Contains(ua, "Firefox/") || strings.Contains(ua, "FxiOS/"):
		return "Firefox"
	case strings.Contains(ua, "Chrome/") || strings.Contains(ua, "CriOS/"):
		return "Chrome"
	case strings.Contains(ua, "Safari/"):
		return "Safari"
	default:
		return ""
	}
}

func systemOf(ua string) string {
	switch {
	case strings.Contains(ua, "iPhone"):
		return "iPhone"
	case strings.Contains(ua, "iPad"):
		return "iPad"
	case strings.Contains(ua, "Android"):
		return "Android"
	case strings.Contains(ua, "Windows"):
		return "Windows"
	case strings.Contains(ua, "Mac OS X") || strings.Contains(ua, "Macintosh"):
		return "macOS"
	case strings.Contains(ua, "CrOS"):
		return "ChromeOS"
	case strings.Contains(ua, "Linux") || strings.Contains(ua, "X11"):
		return "Linux"
	default:
		return ""
	}
}
