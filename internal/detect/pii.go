package detect

import (
	"math/big"
	"regexp"
	"strings"

	"github.com/amareshhebbar/rungated/internal/core"
)

type piiRule struct {
	ID         string
	Class      string
	Category   core.Category
	Re         *regexp.Regexp
	Group      int
	Validate   func(string) bool
	Severity   float64
	Confidence float64
	Quick      func(string) bool
	Keywords   []string
	Numeric    bool
}

func digits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func Luhn(num string) bool {
	if len(num) < 12 {
		return false
	}
	sum := 0
	alt := false
	for i := len(num) - 1; i >= 0; i-- {
		d := int(num[i] - '0')
		if alt {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		alt = !alt
	}
	return sum%10 == 0
}

func validCard(s string) bool {
	d := digits(s)
	if len(d) < 13 || len(d) > 19 || !Luhn(d) {
		return false
	}
	if strings.Count(d, string(d[0])) == len(d) {
		return false
	}
	p2 := d[:2]
	p4 := d[:4]
	switch {
	case d[0] == '4':
		return len(d) == 13 || len(d) == 16 || len(d) == 19
	case p2 >= "51" && p2 <= "55", p4 >= "2221" && p4 <= "2720":
		return len(d) == 16
	case p2 == "34" || p2 == "37":
		return len(d) == 15
	case p4 == "6011" || p2 == "65" || (d[:3] >= "644" && d[:3] <= "649"):
		return len(d) >= 16
	case p2 == "35":
		return len(d) >= 16
	case p2 == "36" || p2 == "38" || p2 == "30":
		return len(d) >= 14
	case p2 == "60" || p2 == "81" || p2 == "82" || p2 == "50" || p2 == "62":
		return len(d) >= 16
	}
	return false
}

func validSSN(s string) bool {
	d := digits(s)
	if len(d) != 9 {
		return false
	}
	area, group, serial := d[:3], d[3:5], d[5:]
	if area == "000" || area == "666" || area[0] == '9' || group == "00" || serial == "0000" {
		return false
	}
	if d == "123456789" || d == "078051120" || d == "219099999" {
		return false
	}
	return true
}

func ValidIBAN(s string) bool {
	s = strings.ToUpper(strings.ReplaceAll(s, " ", ""))
	if len(s) < 15 || len(s) > 34 {
		return false
	}
	r := s[4:] + s[:4]
	var b strings.Builder
	for _, c := range r {
		switch {
		case c >= '0' && c <= '9':
			b.WriteRune(c)
		case c >= 'A' && c <= 'Z':
			b.WriteString(big.NewInt(int64(c - 'A' + 10)).String())
		default:
			return false
		}
	}
	n, ok := new(big.Int).SetString(b.String(), 10)
	if !ok {
		return false
	}
	return new(big.Int).Mod(n, big.NewInt(97)).Int64() == 1
}

var verhoeffD = [10][10]int{
	{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}, {1, 2, 3, 4, 0, 6, 7, 8, 9, 5}, {2, 3, 4, 0, 1, 7, 8, 9, 5, 6}, {3, 4, 0, 1, 2, 8, 9, 5, 6, 7}, {4, 0, 1, 2, 3, 9, 5, 6, 7, 8},
	{5, 9, 8, 7, 6, 0, 4, 3, 2, 1}, {6, 5, 9, 8, 7, 1, 0, 4, 3, 2}, {7, 6, 5, 9, 8, 2, 1, 0, 4, 3}, {8, 7, 6, 5, 9, 3, 2, 1, 0, 4}, {9, 8, 7, 6, 5, 4, 3, 2, 1, 0},
}
var verhoeffP = [8][10]int{
	{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}, {1, 5, 7, 6, 2, 8, 3, 0, 9, 4}, {5, 8, 0, 3, 7, 9, 6, 1, 4, 2}, {8, 9, 1, 6, 0, 4, 3, 5, 2, 7},
	{9, 4, 5, 3, 1, 2, 6, 8, 7, 0}, {4, 2, 8, 6, 5, 7, 3, 9, 0, 1}, {2, 7, 9, 3, 8, 0, 6, 4, 1, 5}, {7, 0, 4, 6, 9, 1, 3, 2, 5, 8},
}

func Verhoeff(num string) bool {
	c := 0
	for i := 0; i < len(num); i++ {
		d := int(num[len(num)-1-i] - '0')
		c = verhoeffD[c][verhoeffP[i%8][d]]
	}
	return c == 0
}

func validAadhaar(s string) bool {
	d := digits(s)
	return len(d) == 12 && d[0] >= '2' && Verhoeff(d)
}

func validPhone(s string) bool {
	d := digits(s)
	return len(d) >= 10 && len(d) <= 15 && strings.Count(d, string(d[len(d)-1])) < len(d)-1
}

func validEmail(s string) bool {
	at := strings.LastIndex(s, "@")
	if at < 1 {
		return false
	}
	dom := s[at+1:]
	return strings.Contains(dom, ".") && !strings.HasSuffix(dom, ".png") && !strings.HasSuffix(dom, ".jpg")
}

var piiRules = []piiRule{
	{ID: "email", Class: "pii.email", Category: core.CatPII, Re: regexp.MustCompile(`[A-Za-z0-9._%+\-]{1,64}@[A-Za-z0-9.\-]{1,253}\.[A-Za-z]{2,24}`), Validate: validEmail, Severity: 0.3, Confidence: 0.95, Quick: func(s string) bool { return strings.IndexByte(s, '@') >= 0 }},
	{ID: "ssn", Numeric: true, Class: "pii.ssn", Category: core.CatPII, Re: regexp.MustCompile(`\b(\d{3}-\d{2}-\d{4})\b`), Group: 1, Validate: validSSN, Severity: 0.8, Confidence: 0.85},
	{ID: "ssn-labelled", Keywords: []string{"ssn", "social security"}, Class: "pii.ssn", Category: core.CatPII, Re: regexp.MustCompile(`(?i)\b(?:ssn|social security(?: number| no\.?)?)\b[^0-9]{0,12}(\d{3}[ .]?\d{2}[ .]?\d{4})\b`), Group: 1, Validate: validSSN, Severity: 0.85, Confidence: 0.95},
	{ID: "credit-card", Numeric: true, Class: "pci.card", Category: core.CatPII, Re: regexp.MustCompile(`\b(?:\d[ -]?){12,18}\d\b`), Validate: validCard, Severity: 0.85, Confidence: 0.9},
	{ID: "iban", Class: "pii.iban", Category: core.CatPII, Re: regexp.MustCompile(`\b[A-Z]{2}\d{2}(?: ?[A-Z0-9]{4}){2,7}(?: ?[A-Z0-9]{1,4})?\b`), Validate: ValidIBAN, Severity: 0.6, Confidence: 0.9},
	{ID: "aadhaar", Numeric: true, Class: "pii.aadhaar", Category: core.CatPII, Re: regexp.MustCompile(`\b[2-9]\d{3}[ -]?\d{4}[ -]?\d{4}\b`), Validate: validAadhaar, Severity: 0.8, Confidence: 0.85},
	{ID: "pan", Class: "pii.pan", Category: core.CatPII, Re: regexp.MustCompile(`\b[A-Z]{3}[PCHFATBLJG][A-Z]\d{4}[A-Z]\b`), Severity: 0.6, Confidence: 0.8},
	{ID: "phone-e164", Numeric: true, Class: "pii.phone", Category: core.CatPII, Re: regexp.MustCompile(`\+[1-9]\d{0,2}[ \-.]?\(?\d{2,4}\)?[ \-.]?\d{3,5}[ \-.]?\d{3,5}\b`), Validate: validPhone, Severity: 0.3, Confidence: 0.8},
	{ID: "phone-us", Numeric: true, Class: "pii.phone", Category: core.CatPII, Re: regexp.MustCompile(`(?:^|[^\d+])(\(?[2-9]\d{2}\)?[\-. ][2-9]\d{2}[\-. ]\d{4})\b`), Group: 1, Validate: validPhone, Severity: 0.3, Confidence: 0.75},
	{ID: "dob", Keywords: []string{"dob", "d.o.b", "birth", "born"}, Class: "pii.dob", Category: core.CatPII, Re: regexp.MustCompile(`(?i)\b(?:dob|d\.o\.b\.|date of birth|birth ?date|born on)\b[:\s]*(\d{1,4}[/\-.]\d{1,2}[/\-.]\d{1,4})`), Group: 1, Severity: 0.4, Confidence: 0.85},
	{ID: "us-passport", Keywords: []string{"passport"}, Class: "pii.passport", Category: core.CatPII, Re: regexp.MustCompile(`(?i)\bpassport(?: number| no\.?| #)?\b[^A-Z0-9]{0,6}([A-Z0-9]{6,9})\b`), Group: 1, Severity: 0.6, Confidence: 0.8},
	{ID: "mrn", Keywords: []string{"mrn", "medical record"}, Class: "phi.mrn", Category: core.CatPHI, Re: regexp.MustCompile(`(?i)\b(?:mrn|medical record(?: number| no\.?)?)\b[:#\s]*([A-Z0-9\-]{5,20})\b`), Group: 1, Severity: 0.7, Confidence: 0.85},
	{ID: "diagnosis-icd10", Keywords: []string{"diagnos", "icd", "dx"}, Class: "phi.diagnosis", Category: core.CatPHI, Re: regexp.MustCompile(`(?i)\b(?:diagnos\w*|icd[- ]?10|dx)\b[^.\n]{0,40}?\b([A-TV-Z]\d{2}(?:\.\d{1,4})?)\b`), Group: 1, Severity: 0.6, Confidence: 0.75},
	{ID: "health-plan-id", Keywords: []string{"member id", "subscriber id", "policy", "medicaid", "medicare"}, Class: "phi.member_id", Category: core.CatPHI, Re: regexp.MustCompile(`(?i)\b(?:member id|subscriber id|policy (?:number|no\.?)|medicaid id|medicare (?:number|id))\b[:#\s]*([A-Z0-9\-]{6,20})\b`), Group: 1, Severity: 0.6, Confidence: 0.8},
}

type PIIDetector struct{}

func (PIIDetector) Name() string { return "pii" }

func (PIIDetector) Scan(part int, text string) []core.Finding {
	if len(text) < 5 {
		return nil
	}
	hasDigit := strings.ContainsAny(text, "0123456789")
	var lower string
	runs := numericRuns(text)
	var out []core.Finding
	var taken [][2]int
	counts := map[string]int{}
	for _, r := range piiRules {
		if r.Quick != nil && !r.Quick(text) {
			continue
		}
		if r.ID != "email" && r.ID != "pan" && !hasDigit {
			continue
		}
		if len(r.Keywords) > 0 {
			if lower == "" {
				lower = strings.ToLower(text)
			}
			hit := false
			for _, k := range r.Keywords {
				if strings.Contains(lower, k) {
					hit = true
					break
				}
			}
			if !hit {
				continue
			}
		}
		type match struct{ s, e int }
		var ms []match
		collect := func(sub string, off int) {
			for _, m := range r.Re.FindAllStringSubmatchIndex(sub, 256) {
				s, e := m[0], m[1]
				if r.Group > 0 {
					s, e = m[2*r.Group], m[2*r.Group+1]
				}
				if s >= 0 {
					ms = append(ms, match{s + off, e + off})
				}
			}
		}
		if r.ID == "email" {
			for _, em := range findEmails(text) {
				ms = append(ms, match{em[0], em[1]})
			}
		} else if r.Numeric {
			for _, rn := range runs {
				lo := rn[0]
				if lo > 0 {
					lo--
				}
				hi := rn[1]
				if hi < len(text) {
					hi++
				}
				collect(text[lo:hi], lo)
			}
		} else {
			collect(text, 0)
		}
		for _, mt := range ms {
			s, e := mt.s, mt.e
			v := text[s:e]
			if r.Validate != nil && !r.Validate(v) {
				continue
			}
			if strings.HasPrefix(v, "[[") || (s >= 2 && text[s-2:s] == "[[") {
				continue
			}
			ov := false
			for _, t := range taken {
				if s < t[1] && e > t[0] {
					ov = true
					break
				}
			}
			if ov {
				continue
			}
			taken = append(taken, [2]int{s, e})
			counts[r.Class]++
			out = append(out, core.Finding{
				Detector:   "pii",
				Tier:       0,
				Category:   r.Category,
				Rule:       r.ID,
				Severity:   r.Severity,
				Confidence: r.Confidence,
				Message:    "sensitive data detected: " + r.Class,
				DataClass:  r.Class,
				Span:       &core.Span{Part: part, Start: s, End: e},
				Redactable: true,
				Match:      v,
			})
		}
	}
	total := 0
	for _, c := range counts {
		total += c
	}
	if total >= 5 && bulkRecords(text) {
		out = append(out, core.Finding{
			Detector:   "pii",
			Tier:       0,
			Category:   core.CatExfil,
			Rule:       "bulk-records",
			Severity:   0.8,
			Confidence: 0.85,
			Message:    "bulk personal records detected in a single payload",
			DataClass:  "data.bulk_records",
		})
	}
	return out
}

var rowSepRe = regexp.MustCompile(`[\n;]|\},\s*\{|\],\s*\[`)

func bulkRecords(text string) bool {
	rows := rowSepRe.Split(text, -1)
	withPII := 0
	for _, r := range rows {
		if strings.Contains(r, "@") || strings.Count(digits(r), "") > 9 {
			withPII++
		}
	}
	return withPII >= 4
}

func isNumChar(c byte) bool {
	return (c >= '0' && c <= '9') || c == ' ' || c == '-' || c == '.' || c == '(' || c == ')' || c == '+'
}

func numericRuns(text string) [][2]int {
	var out [][2]int
	i := 0
	n := len(text)
	for i < n {
		c := text[i]
		if !((c >= '0' && c <= '9') || c == '+' || c == '(') {
			i++
			continue
		}
		j := i
		d := 0
		for j < n && isNumChar(text[j]) {
			if text[j] >= '0' && text[j] <= '9' {
				d++
			}
			j++
		}
		e := j
		for e > i && !(text[e-1] >= '0' && text[e-1] <= '9') && text[e-1] != ')' {
			e--
		}
		if d >= 9 {
			out = append(out, [2]int{i, e})
		}
		if j == i {
			j++
		}
		i = j
	}
	return out
}

func emailLocal(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '.' || c == '_' || c == '%' || c == '+' || c == '-'
}

func emailDomain(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '.' || c == '-'
}

func findEmails(text string) [][2]int {
	var out [][2]int
	from := 0
	for {
		k := strings.IndexByte(text[from:], '@')
		if k < 0 {
			return out
		}
		at := from + k
		s := at
		for s > 0 && at-s < 64 && emailLocal(text[s-1]) {
			s--
		}
		e := at + 1
		for e < len(text) && e-at < 254 && emailDomain(text[e]) {
			e++
		}
		for e > at+1 && (text[e-1] == '.' || text[e-1] == '-') {
			e--
		}
		dom := text[at+1 : e]
		dot := strings.LastIndexByte(dom, '.')
		if s < at && dot > 0 && len(dom)-dot-1 >= 2 {
			tld := dom[dot+1:]
			ok := true
			for i := 0; i < len(tld); i++ {
				if !((tld[i] >= 'a' && tld[i] <= 'z') || (tld[i] >= 'A' && tld[i] <= 'Z')) {
					ok = false
				}
			}
			if ok {
				out = append(out, [2]int{s, e})
			}
		}
		from = at + 1
		if len(out) > 256 {
			return out
		}
	}
}
