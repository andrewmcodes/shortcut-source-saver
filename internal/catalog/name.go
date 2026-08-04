package catalog

import (
	"regexp"
	"strings"
	"unicode"
)

var appAndIntentIdentifier = regexp.MustCompile(`^.+?\..+?\.(.+?)\.(.*)$`)

func DisplayName(identifier string) string {
	if !IsApple(identifier) {
		if match := appAndIntentIdentifier.FindStringSubmatch(identifier); match != nil {
			app, intent := match[1], match[2]
			return app + " > " + intent
		}
		return prettify(identifier)
	}
	for _, prefix := range appleIdentifierPrefixes {
		if withoutPrefix, ok := strings.CutPrefix(identifier, prefix); ok {
			return prettify(withoutPrefix)
		}
	}
	return prettify(identifier)
}

func isSegmentSeparator(r rune) bool {
	return r == '.' || r == '_' || r == ' '
}

func prettify(s string) string {
	var titled []string
	for _, segment := range strings.FieldsFunc(s, isSegmentSeparator) {
		for i, hyphenated := range strings.Split(segment, "-") {
			words := splitWords(hyphenated)
			for j := range words {
				words[j] = titleCase(words[j])
			}
			joined := strings.Join(words, " ")
			if i > 0 && len(titled) > 0 {
				titled[len(titled)-1] += "-" + joined
				continue
			}
			titled = append(titled, joined)
		}
	}
	return strings.Join(titled, " ")
}

func splitWords(segment string) []string {
	if segment == "" {
		return nil
	}
	if hasUpper(segment) {
		return splitCamelCase(segment)
	}
	if words := splitVocabulary(segment); words != nil {
		return words
	}
	return []string{segment}
}

func hasUpper(s string) bool {
	for _, r := range s {
		if unicode.IsUpper(r) {
			return true
		}
	}
	return false
}

func splitCamelCase(s string) []string {
	runes := []rune(s)
	var words []string
	wordStart := 0
	for i := 1; i < len(runes); i++ {
		previous, current := runes[i-1], runes[i]
		startsNewWord := false
		switch {
		case unicode.IsUpper(current) && !unicode.IsUpper(previous):
			startsNewWord = true
		case unicode.IsUpper(previous) && unicode.IsUpper(current) && i+1 < len(runes) && unicode.IsLower(runes[i+1]):
			startsNewWord = true
		case unicode.IsDigit(current) && !unicode.IsDigit(previous):
			startsNewWord = true
		}
		if startsNewWord {
			words = append(words, string(runes[wordStart:i]))
			wordStart = i
		}
	}
	return append(words, string(runes[wordStart:]))
}

func splitVocabulary(s string) []string {
	const uncoverable = -1
	wordCount := make([]int, len(s)+1)
	wordStart := make([]int, len(s)+1)
	for end := 1; end <= len(s); end++ {
		wordCount[end] = uncoverable
	}
	for end := 1; end <= len(s); end++ {
		for start := 0; start < end; start++ {
			if wordCount[start] == uncoverable {
				continue
			}
			if !vocabulary[s[start:end]] {
				continue
			}
			if wordCount[end] == uncoverable || wordCount[start]+1 < wordCount[end] {
				wordCount[end] = wordCount[start] + 1
				wordStart[end] = start
			}
		}
	}
	if wordCount[len(s)] == uncoverable {
		return nil
	}
	words := make([]string, 0, wordCount[len(s)])
	for end := len(s); end > 0; end = wordStart[end] {
		words = append(words, s[wordStart[end]:end])
	}
	for left, right := 0, len(words)-1; left < right; left, right = left+1, right-1 {
		words[left], words[right] = words[right], words[left]
	}
	return words
}

var acronyms = map[string]string{
	"api": "API", "css": "CSS", "csv": "CSV", "dnd": "DND", "gif": "GIF", "gps": "GPS",
	"html": "HTML", "http": "HTTP", "https": "HTTPS", "id": "ID", "ip": "IP",
	"jpeg": "JPEG", "js": "JS", "json": "JSON", "mms": "MMS", "nfc": "NFC", "ocr": "OCR",
	"pdf": "PDF", "png": "PNG", "qr": "QR", "rss": "RSS", "sms": "SMS", "ssh": "SSH",
	"svg": "SVG", "tv": "TV", "ui": "UI", "url": "URL", "usb": "USB", "uuid": "UUID",
	"vpn": "VPN", "xml": "XML",
}

var irregularCapitalisation = map[string]string{
	"ios": "iOS", "iphone": "iPhone", "ipad": "iPad", "macos": "macOS",
	"applescript": "AppleScript", "javascript": "JavaScript", "airdrop": "AirDrop",
	"airplay": "AirPlay", "facetime": "FaceTime", "icloud": "iCloud", "wifi": "Wi-Fi",
	"base64": "Base64",
}

func titleCase(word string) string {
	if word == "" {
		return ""
	}
	lower := strings.ToLower(word)
	if irregular, ok := irregularCapitalisation[lower]; ok {
		return irregular
	}
	if acronym, ok := acronyms[lower]; ok {
		return acronym
	}
	if hasUpper(word) {
		return word
	}
	runes := []rune(word)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

var vocabularyWords = strings.Fields(`
	accessory account acknowledge action actions activity add address adjust airdrop
	airplane airplay alarm album albums alert all always and any api app appearance apps
	archive area artist ask aspect at attribute audio automation available average back
	background backup barcode base base64 battery begins between blank blur bluetooth
	body book bookmark boolean border box brightness browser button by calculate
	calendar call camera cancel capitalize card case cast cell cellular center change
	character characters chart chat check choose class clean clear clipboard clock close
	closed cloud code coerce collection color colour column combine command comment
	compare compress condition conditional connect contact contacts contains content
	continue control convert copy count country create credential crop csv currency
	current custom cut data date dates day days decode decrypt default delay delete
	description detect device dial dictate dictionary did digit dim direction directions
	disable disconnect discover display distance dnd do document does done door download
	drive driving duplicate duration edit email empty enable encode encrypt end ends
	enter entry enumeration event events every exit expand export exposure extension
	extract face facetime fail favorite feed field file files fill filter find first
	fit flash flashlight flip focus folder font for form format forward frame framework
	from front full function generate get gif gist go goal google gps gradient graph gray
	grid group
	guide has hash have header health height hex hide history hold home hour hours html
	http https icloud icon id if image images import in index info input insert install
	instant intent invert ip is item items java javascript join json jump keep key
	keyboard keys kind label language last latest launch layer left length less level
	library light lights line lines link list lists live load local location lock log
	login look loop low lower lowercase mac macos magnifier mail make map maps mark
	markup mask match math max measurement media medium menu merge message messages
	memo memos metadata middle min minute minutes mobile mode modify month months more
	move movie music
	mute my name names navigate navigation nearby network new next nfc night no not note
	notes nothing notification notifications now number numbers ocr of off offset ok on
	one open option options or order orientation out output overlay pad page pages
	parse pass passbook password paste path pause payment pdf percent percentage
	permission person phone photo photos pick pin place places play playlist png podcast
	point pointer position post power prepend preview previous price print priority
	profile progress prompt property provider public purchase push qr question queue
	quick quit quote radio random range rate read reader receive recent recents record
	redirect reduce refresh regex region reload remainder reminder reminders remote
	remove rename repeat replace reply report request reset resize resolve respond
	response restart restore result resume return reveal reverse rewind rgb right ring
	role room root rotate round route row rss run running sample save scan scene
	schedule scheme screen screenshot script scroll search second seconds section seek
	select send sensor sentence series server service session set setting settings shape
	share sheet shell shift short shortcut shortcuts show shuffle shutdown sign signal
	silence simulate single size skip sleep slide slider slow sms snapshot social song
	songs sort sound source space speak speaker speech speed spell split spoken ssh
	stack star start state station status step steps stop stopwatch storage store
	stream string strip style submit subtract suffix sum summary sun support svg swap
	swipe switch sync system tab table tabs tag tags take tap target task tell
	temperature template terminal text theme then thing thumbnail time timer timeline
	times timestamp title to today toggle token torch total touch track transcribe
	transform translate trash trigger trim true turn tv type ui unarchive undo unit
	units unlock unmute unzip up update upload upper uppercase url urls usb use user
	uuid validate value values variable variables verify version vibrate video view
	visible voice volume vpn wait wake wallpaper watch water weather web week weeks
	weight when while white width wifi window with word words work workflow workout
	wrap write xml year years zip zone zoom
`)

var vocabulary = func() map[string]bool {
	words := make(map[string]bool, len(vocabularyWords))
	for _, word := range vocabularyWords {
		words[word] = true
	}
	return words
}()
