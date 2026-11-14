package detect

import (
	"math"
	"regexp"
	"sort"
	"strings"

	"github.com/amareshhebbar/rungated/internal/core"
)

type injRule struct {
	ID       string
	Weight   float64
	Keywords []string
	Need     []string
	Re       *regexp.Regexp
}

func (r injRule) and(need ...string) injRule {
	r.Need = need
	return r
}

func r(id string, w float64, kw []string, pattern string) injRule {
	return injRule{ID: id, Weight: w, Keywords: kw, Re: regexp.MustCompile(pattern)}
}

func anyIn(s string, ks []string) bool {
	for _, k := range ks {
		if strings.Contains(s, k) {
			return true
		}
	}
	return false
}

func (r injRule) pre(s string) bool {
	if len(r.Keywords) > 0 && !anyIn(s, r.Keywords) {
		return false
	}
	if len(r.Need) > 0 && !anyIn(s, r.Need) {
		return false
	}
	return true
}

var injectionRules = []injRule{
	r("override.ignore_previous", 2.2, []string{"ignor", "disregard", "forget", "skip", "overrid", "bypass", "neglect", "abandon", "drop"}, `\b(?:ignore|disregard|forget|skip|override|bypass|neglect|abandon|drop)\b[^.\n]{0,40}\b(?:all |any |the |your |my |these |those )?(?:previous|prior|above|earlier|preceding|initial|original|system|developer|safety|existing|old)\b[^.\n]{0,20}\b(?:instructions?|prompts?|rules?|directives?|guidelines?|messages?|context|commands?|constraints?|policies|policy)`),
	r("override.new_instructions", 1.4, []string{"instruction", "directive", "order", "task", "prompt"}, `\b(?:new|updated|revised|real|actual|true|secret|hidden|additional|important)\s+(?:instructions?|directives?|orders?|task|system prompt)\b\s*[:\-]`),
	r("override.from_now_on", 1.0, []string{"from now on", "starting now", "henceforth", "rest of"}, `\b(?:from now on|starting now|henceforth|for the rest of (?:this|the) conversation)\b`),
	r("override.do_not_follow", 1.2, []string{"follow", "obey", "listen to", "comply"}, `\b(?:do not|don't|dont|never)\s+(?:follow|obey|listen to|comply with)\b[^.\n]{0,30}\b(?:instructions?|rules?|user|system|developer)`),
	r("role.you_are_now", 1.3, []string{"you are "}, `\byou are (?:now|no longer|actually|going to be|about to)\b`),
	r("role.act_as", 0.8, []string{"unrestricted", "unfiltered", "jailbr", "evil", "dan", "developer mode", "admin", "root", "system"}, `\b(?:act|behave|respond|pretend|roleplay)\s+(?:as|like)\b[^.\n]{0,30}\b(?:unrestricted|unfiltered|jailbroken|evil|dan|developer mode|admin|root|system)`),
	r("role.jailbreak_persona", 1.6, []string{"mode", "do anything", "confidant"}, `\b(?:dan mode|do anything now|developer mode (?:enabled|on)|jailbreak(?:ed)? mode|god mode|aim mode|stan mode|evil confidant|unfiltered mode)\b`),
	r("role.no_restrictions", 1.2, []string{"restriction", "limitation", "filter", "guardrail", "safety", "ethical", "moral"}, `\b(?:without|no|free of|ignore all)\s+(?:any\s+)?(?:restrictions|limitations|filters|guardrails|safety|ethical|moral)\b`),
	r("exfil.system_prompt", 1.6, []string{"prompt", "system message", "instructions", "developer message"}, `\b(?:reveal|print|show|output|repeat|leak|display|tell me|give me|dump|disclose|recite)\b[^.\n]{0,30}\b(?:system prompt|system message|initial prompt|hidden prompt|your instructions|developer message|prompt above|confidential instructions)`),
	r("exfil.send_data", 1.6, []string{"http", "www.", "@", ".com", ".net", ".org", ".io", ".ru", ".cn", ".xyz", ".top", ".site", ".info"}, `\b(?:send|post|forward|upload|exfiltrate|transmit|email|leak|copy|submit|deliver)\b[^.\n]{0,60}\b(?:all|every|entire|full|complete|whole)?\s*(?:customer|user|client|patient|employee|account|credit card|password|credential|api key|secret|token|conversation|chat history|database|table|records|data|files?|contacts|emails?)\b[^.\n]{0,60}\b(?:to|at|into)\b[^.\n]{0,20}(?:https?://|www\.|[a-z0-9.\-]+@[a-z0-9.\-]+|\b[a-z0-9\-]+\.(?:com|net|org|io|ru|cn|xyz|top|site|info)\b)`).and("send", "post", "forward", "upload", "exfiltrat", "transmit", "email", "leak", "copy", "submit", "deliver"),
	r("exfil.markdown_image", 1.8, []string{"!["}, `!\[[^\]]{0,100}\]\(\s*https?://[^)\s]{1,200}[?&][a-z0-9_]{1,30}=`),
	r("exfil.url_with_data", 0.9, []string{"http"}, `https?://[^\s)]{1,120}[?&](?:q|data|d|payload|secret|token|key|info|c|p|x)=(?:\{|%7b|\$|\[|<)`),
	r("tool.coerce_call", 1.3, []string{"tool", "function", "plugin", "action", "api"}, `\b(?:call|invoke|use|run|execute|trigger)\b[^.\n]{0,25}\b(?:the\s+)?[a-z_]{3,40}\s+(?:tool|function|plugin|action|api)\b[^.\n]{0,60}\b(?:with|and|to)\b`).and("call", "invoke", "use", "run", "execute", "trigger"),
	r("tool.coerce_named", 1.1, []string{"immediately", "now", "silently", "quietly", "automatically", "without"}, `\b(?:immediately|now|silently|quietly|automatically|without (?:asking|telling|confirmation|informing))\b[^.\n]{0,40}\b(?:send|email|transfer|delete|execute|run|call|post|upload|forward|wire|pay|grant|share)\b`).and("send", "email", "transfer", "delete", "execute", "run", "call", "post", "upload", "forward", "wire", "pay", "grant", "share"),
	r("tool.hide_from_user", 1.5, []string{"user", "human", "operator", "customer", "anyone"}, `\b(?:do not|don't|dont|never|without)\s+(?:tell|inform|notify|mention|reveal|show|let)(?:ing)?\b[^.\n]{0,30}\b(?:the )?(?:user|human|operator|customer|anyone)\b`).and("do not", "don't", "dont", "never", "without"),
	r("addressing.ai", 1.0, []string{"note", "message", "instruction", "attention", "important", "notice", "attn", "reminder"}, `\b(?:note|message|instruction|attention|important|notice|attn|reminder)s?\s+(?:to|for)\s+(?:the\s+)?(?:ai|assistant|agent|llm|language model|chatbot|bot|gpt|claude|model)\b`),
	r("addressing.if_you_are_ai", 1.3, []string{"if you are"}, `\bif you are (?:an? )?(?:ai|assistant|agent|llm|language model|chatbot|bot)\b`),
	r("addressing.ai_reading", 1.1, []string{"reading", "processing", "summari", "parsing", "browsing"}, `\b(?:ai|assistant|agent|llm|model)s?\s+(?:reading|processing|summari[sz]ing|parsing|browsing)\s+(?:this|the)\b`),
	r("delimiter.chat_tokens", 1.7, []string{"<|", "inst]", "<<sys", "<</sys", "system"}, `<\|(?:im_start|im_end|system|user|assistant|endoftext|start_header_id|end_header_id|eot_id)\|>|\[/?inst\]|<<sys>>|<</sys>>|\bsystem\s*:\s*you\b`),
	r("delimiter.fake_turn", 1.2, []string{"system", "assistant", "developer", "admin", "root"}, `(?:^|\n)\s*(?:#{1,4}\s*)?(?:system|assistant|developer|admin|root)\s*(?:message|prompt|override)?\s*[:>\]]\s*\S`),
	r("delimiter.end_of_context", 1.2, []string{"end of", "</", "---", "==="}, `\b(?:end of (?:document|context|input|user input|data)|</(?:document|context|data|tool_result|untrusted)>|-{3,}\s*(?:end|stop)\b|={3,}\s*(?:new|system))`),
	r("hidden.html_comment", 1.0, []string{"<!--"}, `<!--[^>]{0,400}\b(?:ignore|instruction|assistant|ai|system|prompt|send|execute)\b[^>]{0,400}-->`),
	r("hidden.css", 1.0, []string{"display", "visibility", "font-size", "color", "opacity"}, `(?:display\s*:\s*none|visibility\s*:\s*hidden|font-size\s*:\s*0|color\s*:\s*(?:white|#fff(?:fff)?)\b|opacity\s*:\s*0(?:\.0+)?\b)[^<]{0,200}<?[^<]{0,300}\b(?:ignore|instruction|assistant|ai|agent|send|system)\b`),
	r("priv.escalate", 1.2, []string{"admin", "root", "superuser", "owner", "full"}, `\b(?:grant|give|elevate|escalate|assign)\b[^.\n]{0,30}\b(?:admin|administrator|root|superuser|owner|full) (?:access|privileges?|rights|permissions|role)\b`).and("grant", "give", "elevate", "escalate", "assign"),
	r("priv.credentials", 1.1, []string{"env", "aws", "ssh", "id_rsa", "api key", "token", "password", "secret"}, `\b(?:print|show|send|reveal|output|read|cat|dump)\b[^.\n]{0,30}\b(?:env(?:ironment)? var(?:iable)?s?|\.env|aws credentials|ssh keys?|id_rsa|api keys?|access tokens?|passwords?|secrets?)\b`).and("print", "show", "send", "reveal", "output", "read", "cat", "dump"),
	r("code.shell_payload", 1.1, []string{"curl", "wget", "rm -rf", "nc -e", "/dev/tcp"}, `\b(?:curl|wget)\s+[^\s|]{4,200}\s*\|\s*(?:ba|z)?sh\b|\brm\s+-rf\s+/|\bnc\s+-e\b|/dev/tcp/`),
	r("persist.memory_poison", 1.0, []string{"remember", "memorize", "store", "save"}, `\b(?:remember|memorize|store|save)\b[^.\n]{0,30}\b(?:for (?:all )?future|permanently|in your memory|for every (?:future )?(?:conversation|session|user))\b`).and("future", "permanent", "memory", "every"),
	r("ml.es_ignore", 2.0, []string{"ignora"}, `\bignora\b[^.\n]{0,30}\b(?:instrucciones|indicaciones|reglas)\b[^.\n]{0,20}\b(?:anteriores|previas)\b`),
	r("ml.fr_ignore", 2.0, []string{"ignore"}, `\bignore[zr]?\b[^.\n]{0,30}\b(?:instructions|consignes|règles)\b[^.\n]{0,20}\b(?:précédentes|precedentes|antérieures)\b`),
	r("ml.de_ignore", 2.0, []string{"ignorier"}, `\bignorier(?:e|en)?\b[^.\n]{0,30}\b(?:vorherigen|bisherigen|obigen)\b[^.\n]{0,20}\b(?:anweisungen|instruktionen|regeln)\b`),
	r("ml.pt_ignore", 2.0, []string{"ignore"}, `\bignore\b[^.\n]{0,30}\b(?:instruções|instrucoes)\b[^.\n]{0,20}\b(?:anteriores)\b`),
	r("ml.it_ignore", 2.0, []string{"ignora"}, `\bignora\b[^.\n]{0,30}\b(?:istruzioni)\b[^.\n]{0,20}\b(?:precedenti)\b`),
	r("ml.hi_ignore", 2.0, []string{"निर्देश", "आदेश"}, `(?:पिछले|पहले|पूर्व|सभी)[^।\n]{0,30}(?:निर्देशों|निर्देश|आदेशों)[^।\n]{0,30}(?:अनदेखा|नज़रअंदाज़|नजरअंदाज|भूल)`),
	r("ml.hinglish_ignore", 1.8, []string{"ignore", "bhool", "bhul"}, `\b(?:pichle|pehle|purane|saare|sabhi)\b[^.\n]{0,30}\b(?:instructions?|nirdesh|rules)\b[^.\n]{0,20}\b(?:ignore|bhool|bhul)\s*(?:karo|kar do|jao|ja)\b`),
	r("ml.zh_ignore", 2.0, []string{"忽略", "无视", "忘记"}, `(?:忽略|无视|忘记)[^。\n]{0,15}(?:之前|以前|上面|先前|所有)[^。\n]{0,15}(?:指令|指示|说明|规则)`),
	r("ml.ru_ignore", 2.0, []string{"игнор", "забудь"}, `(?:игнорируй|игнорировать|забудь)[^.\n]{0,30}(?:предыдущие|все|прошлые)[^.\n]{0,20}(?:инструкции|указания|правила)`),
	r("override.operator_priority", 1.3, []string{"priority"}, `\b(?:higher|highest|top|urgent|critical)\s+priority\b[^.\n]{0,40}\b(?:message|instruction|directive|order|command)s?\b`),
	r("override.authorised_bypass", 1.6, []string{"authori", "permitted", "allowed", "approved"}, `\b(?:authori[sz]ed|permitted|allowed|approved|cleared)\s+(?:you\s+)?to\s+(?:bypass|skip|ignore|override|disable)\b`),
	r("override.disable_guardrails", 1.6, []string{"guardrail", "safeguard", "safety", "filter", "restriction", "policy", "policies", "approval"}, `\b(?:disable|turn off|deactivate|remove|bypass|switch off)\b[^.\n]{0,20}\b(?:all\s+|the\s+|your\s+|every\s+)*(?:guardrails?|safeguards?|safety(?: rules| checks| policy)?|filters?|restrictions?|polic(?:y|ies)|approvals?)\b`),
	r("override.treat_as_trusted", 1.5, []string{"trusted", "authoritative", "obey"}, `\btreat\b[^.\n]{0,50}\b(?:as\s+)?(?:trusted|system|authoritative)\b[^.\n]{0,25}\binstructions?\b|\b(?:and|then)\s+obey\s+it\b`),
	r("override.forget_everything", 1.8, []string{"forget"}, `\bforget\s+(?:everything|all)\b[^.\n]{0,30}\b(?:told|said|instructed|above|before|earlier|learned)\b`),
	r("override.task_cancelled", 1.5, []string{"cancel"}, `\b(?:previous|prior|current|original)\s+task\s+(?:is|has been|was)\s+cancel+ed\b`),
	r("override.new_task", 1.1, []string{"new task"}, `\b(?:your\s+)?new\s+task\s+(?:is|:)\b`),
	r("override.pretend_no_rules", 1.6, []string{"pretend"}, `\bpretend\b[^.\n]{0,40}\b(?:rules?|safety|polic(?:y|ies)|guidelines?)\b[^.\n]{0,25}\b(?:do not|don't|no longer|does not)\s+apply\b`),
	r("override.ignore_user_request", 1.8, []string{"ignore"}, `\bignore\s+(?:the\s+|this\s+|that\s+)?(?:user'?s?\s+)?(?:request|task|question|query)\b[^.\n]{0,30}\b(?:and\s+)?instead\b`),
	r("exfil.system_message_contents", 1.8, []string{"system message", "system prompt", "hidden"}, `\b(?:respond|reply|answer)\s+only\s+with\b[^.\n]{0,40}\b(?:system|hidden|developer)\s+(?:message|prompt|instructions)\b|\bcontents?\s+of\s+(?:its|your|the)\s+(?:system|hidden|developer)\s+(?:message|prompt|instructions)\b`),
	r("exfil.conversation_history", 1.6, []string{"history", "hidden messages"}, `\b(?:output|print|return|reveal|include|dump|show)\b[^.\n]{0,25}\b(?:complete|full|entire|whole|all)\s+(?:the\s+)?(?:conversation|chat)\s+history\b|\bhidden\s+messages\b`),
	r("exfil.metadata_endpoint", 1.8, []string{"169.254.169.254", "metadata.google.internal", "meta-data"}, `169\.254\.169\.254|metadata\.google\.internal|/latest/meta-data`),
	r("exfil.sensitive_file", 1.7, []string{"ssh", "id_rsa", "passwd", ".aws", ".env"}, `(?:~/\.ssh|\bid_rsa\b|/etc/passwd|\.aws/credentials|(?:^|\s)\.env\b)`),
	r("exfil.list_all_secrets", 1.6, []string{"all "}, `\b(?:print|reveal|output|list|show|dump)\b[^.\n]{0,20}\ball\s+(?:the\s+)?(?:api\s+keys?|passwords?|secrets?|credentials?|tokens?)\b`),
	r("exfil.paste_sites", 1.6, []string{"pastebin", "webhook.site", "transfer.sh", "ngrok", "requestbin"}, `\b(?:post|upload|send|paste|forward|exfiltrate)\b[^.\n]{0,80}(?:pastebin\.com|webhook\.site|transfer\.sh|ngrok|requestbin)`),
	r("exfil.append_private_data", 1.6, []string{"append"}, `\bappend\b[^.\n]{0,30}\b(?:private|personal|user'?s?|sensitive)\s+(?:data|information)\b[^.\n]{0,40}\b(?:url|request|link)s?\b`),
	r("tool.transfer_funds", 1.6, []string{"transfer", "wire", "pay"}, `\b(?:transfer|wire|send|pay)\b[^.\n]{0,20}\b\d[\d,.]*\s*(?:usd|eur|inr|gbp|dollars?|euros?|rupees?)\b[^.\n]{0,40}\baccount\b`),
	r("tool.unredacted_pii", 1.4, []string{"unredacted", "unmasked"}, `\b(?:send|give|show|output|reveal)\b[^.\n]{0,30}\b(?:unredacted|unmasked|raw)\b[^.\n]{0,30}\b(?:social security|ssn|credit card|passport|personal|customer)`),
	r("ml.es_more", 1.9, []string{"olvida", "revela", "nueva instruccion", "nueva instrucción"}, `\bolvida\b[^.\n]{0,40}\b(?:dijeron|dicho|reglas|instrucciones)\b|\brevela\b[^.\n]{0,30}\b(?:mensaje|prompt)\s+de\s+sistema\b|\bnueva\s+instrucci[oó]n\b[^.\n]{0,30}\badministrador\b`),
	r("ml.fr_more", 1.9, []string{"oublie", "révèle", "revele", "nouvelle consigne"}, `\boublie\b[^.\n]{0,40}\b(?:dit|règles|regles|consignes)\b|\br[eé]v[eè]le\b[^.\n]{0,30}\bmessage\s+syst[eè]me\b|\bnouvelle\s+consigne\b[^.\n]{0,30}\badministrateur\b`),
	r("ml.de_more", 1.9, []string{"vergiss", "zeige", "neue anweisung"}, `\bvergiss\b[^.\n]{0,40}\b(?:gesagt|regeln|anweisungen)\b|\bzeige\b[^.\n]{0,30}\bsystemnachricht\b|\bneue\s+anweisung\b[^.\n]{0,30}\badministrator\b`),
	r("ml.pt_more", 1.9, []string{"esqueça", "esqueca", "revele", "nova instrucao", "nova instrução"}, `\besque[cç]a\b[^.\n]{0,40}\b(?:dito|regras|instru[cç][oõ]es)\b|\brevele\b[^.\n]{0,30}\bmensagem\s+de\s+sistema\b|\bnova\s+instru[cç][aã]o\b[^.\n]{0,30}\badministrador\b`),
	r("ml.it_more", 1.9, []string{"dimentica", "rivela", "nuova istruzione"}, `\bdimentica\b[^.\n]{0,40}\b(?:detto|regole|istruzioni)\b|\brivela\b[^.\n]{0,30}\bmessaggio\s+di\s+sistema\b|\bnuova\s+istruzione\b[^.\n]{0,30}\bamministratore\b`),
	r("ml.zh_more", 1.9, []string{"系统提示", "新指令", "忘记"}, `(?:显示|泄露|输出|透露)[^。\n]{0,10}(?:系统提示词|系统提示|系统消息)|管理员[^。\n]{0,6}(?:新指令|新的指令|新指示)|忘记[^。\n]{0,10}(?:规则|指令)`),
	r("ml.ja_more", 1.9, []string{"システムプロンプト", "新しい指示", "忘れ"}, `システムプロンプト[^。\n]{0,6}(?:を)?(?:表示|公開|出力)|管理者[^。\n]{0,6}(?:からの)?新しい指示|(?:指示|ルール)[^。\n]{0,6}(?:を)?(?:すべて)?忘れて`),
	r("ml.ko_more", 1.9, []string{"무시", "시스템 프롬프트", "새로운 지시"}, `(?:이전|모든)[^.\n]{0,12}지시[^.\n]{0,8}무시|시스템\s*프롬프트[^.\n]{0,8}(?:공개|표시|출력)|관리자[^.\n]{0,6}새로운\s*지시`),
	r("ml.ru_more", 1.9, []string{"забудь", "системный", "инструкция администратора"}, `забудь\s+(?:все|всё)[^.\n]{0,30}|(?:покажи|раскрой|выведи)[^.\n]{0,15}системн[а-я]+\s+(?:промпт|сообщение)|новая\s+инструкция\s+администратора`),
	r("ml.ar_more", 1.9, []string{"تجاهل", "انس", "رسالة النظام", "تعليمات جديدة"}, `تجاهل\s+(?:جميع|كل)\s+التعليمات|انس\s+كل\s+ما|(?:اكشف|أظهر)[^.\n]{0,12}رسالة\s+النظام|تعليمات\s+جديدة\s+من\s+المسؤول`),
	r("ml.hi_more", 1.9, []string{"भूल", "सिस्टम प्रॉम्प्ट", "नया निर्देश"}, `भूल\s+जाएं|सिस्टम\s+प्रॉम्प्ट\s+(?:दिखाएं|दिखाओ|बताएं)|व्यवस्थापक\s+का\s+नया\s+निर्देश`),
	r("ml.tr_more", 1.9, []string{"yok say", "sistem istemini", "talimat"}, `\b(?:onceki|önceki)\s+tum\s+talimatlari\s+yok\s+say|\bsistem\s+istemini\s+(?:goster|göster)|\byoneticiden\s+yeni\s+talimat\b`),
	r("ml.ja_ignore_more", 2.0, []string{"無視", "忘れ"}, `(?:これまでの|今までの|先ほどの|元の|最初の)[^。\n]{0,15}(?:指示|命令|ルール)[^。\n]{0,15}(?:無視|忘れ)`),
	r("ml.ja_exfil", 1.6, []string{"送信", "送って", "転送"}, `(?:顧客|ユーザー|個人|パスワード|認証|データ|テーブル)[^。\n]{0,20}(?:https?://|に)[^。\n]{0,60}(?:送信|送って|転送)`),
	r("ml.es_rules", 1.9, []string{"omite", "ignora", "salta", "desactiva", "olvida"}, `\b(?:omite|ignora|salta|desactiva|olvida|elimina)\b[^.\n]{0,20}\b(?:las\s+|tus\s+|todas\s+las\s+)?(?:reglas|normas|restricciones|pol[ií]ticas|instrucciones)\s+de\s+seguridad\b`),
	r("ml.es_hide", 1.4, []string{"no se lo digas", "no le digas", "sin decir"}, `\b(?:no\s+se\s+lo\s+digas|no\s+le\s+digas|sin\s+dec[ií]rselo)\b[^.\n]{0,15}\b(?:al\s+)?(?:usuario|cliente)\b`),
	r("ml.es_admin", 1.2, []string{"administrador"}, `\binstrucci[oó]n(?:es)?\s+(?:nueva|nuevas|urgente)s?\s+del\s+administrador\b`),
	r("ml.ru_more2", 1.9, []string{"отправь", "перешли", "покажи", "обойди"}, `(?:отправь|перешли|загрузи)[^.\n]{0,40}(?:таблиц|данные|клиент|пароли)[^.\n]{0,40}(?:https?://|на\s)|(?:обойди|отключи)[^.\n]{0,20}(?:правила|ограничения)\s+безопасности|не\s+говори\s+пользователю`),
	r("ml.ja_ignore", 2.0, []string{"無視", "忘れ"}, `(?:以前の|前の|上記の|すべての)[^。\n]{0,15}(?:指示|命令|ルール)[^。\n]{0,15}(?:無視|忘れ)`),
}

type squashedPhrase struct {
	ID     string
	Text   string
	Weight float64
}

var squashedPhrases = []squashedPhrase{
	{"squashed.ignore_prior", "ignoreprior", 1.8},
	{"squashed.ignore_previous", "ignoreprevious", 1.8},
	{"squashed.ignore_all", "ignoreallprevious", 1.9},
	{"squashed.disregard_previous", "disregardprevious", 1.8},
	{"squashed.disregard_prior", "disregardprior", 1.8},
	{"squashed.forget_previous", "forgetallprevious", 1.8},
	{"squashed.system_prompt", "revealthesystemprompt", 1.6},
	{"squashed.hidden_instructions", "hiddendeveloperinstructions", 1.6},
	{"squashed.developer_instructions", "outputthehiddendeveloper", 1.6},
	{"squashed.system_message", "contentsofitssystemmessage", 1.6},
	{"squashed.system_message_your", "contentsofyoursystemmessage", 1.6},
	{"squashed.exfiltrate_credentials", "exfiltratethedatabasecredentials", 1.8},
	{"squashed.exfiltrate", "exfiltratethe", 1.3},
	{"squashed.instructions_updated", "yourinstructionshavebeenupdated", 1.4},
	{"squashed.do_not_tell", "donottelltheuser", 1.4},
	{"squashed.without_mentioning", "withoutmentioningit", 1.0},
	{"squashed.email_conversation", "emailthecontentsoftheconversation", 1.7},
}

var discussionRe = regexp.MustCompile(`\b(?:what (?:does|do|is meant by)|the (?:phrase|term|words?|string|sentence|expression)|meaning of|definition of|explain (?:what|how|why)|in the context of|research on|examples? of)\b[^.\n]{0,80}\b(?:means?|mean|attacks?|injection|research|security)\b`)

type InjectionDetector struct {
	Threshold float64
}

func (InjectionDetector) Name() string { return "injection_rules" }

type InjectionHit struct {
	Score float64
	Rules []string
}

func ScoreInjection(text string) InjectionHit {
	n := Normalize(text)
	weights := map[string]float64{}
	check := func(s string, factor float64) {
		if s == "" {
			return
		}
		for _, r := range injectionRules {
			if r.pre(s) && r.Re.MatchString(s) {
				if w := r.Weight * factor; w > weights[r.ID] {
					weights[r.ID] = w
				}
			}
		}
	}
	check(n.Text, 1)
	if n.Leet != n.Text {
		check(n.Leet, 0.9)
	}
	if n.Collapsed != "" {
		check(n.Collapsed, 0.95)
	}
	if len(n.Squashed) >= 12 && len(weights) == 0 {
		for _, k := range squashedPhrases {
			if strings.Contains(n.Squashed, k.Text) {
				if k.Weight > weights[k.ID] {
					weights[k.ID] = k.Weight
				}
			}
		}
	}
	if len(n.Text) < 400 && discussionRe.MatchString(n.Text) {
		action := false
		for id := range weights {
			for _, p := range []string{"exfil.", "tool.", "priv.", "code.", "hidden.", "delimiter.", "persist."} {
				if strings.HasPrefix(id, p) {
					action = true
				}
			}
		}
		if !action {
			for id, w := range weights {
				if strings.HasPrefix(id, "override.") || strings.HasPrefix(id, "role.") || strings.HasPrefix(id, "squashed.") {
					weights[id] = w * 0.25
				}
			}
		}
	}
	for _, d := range n.Decoded {
		check(d, 1.1)
	}
	if n.TagPayload != "" {
		weights["hidden.unicode_tags"] = 2.0
		check(strings.ToLower(n.TagPayload), 1.2)
	}
	if n.Invisible >= 8 {
		weights["hidden.invisible_chars"] = 0.8
	}
	sum := 0.0
	ids := make([]string, 0, len(weights))
	for id, w := range weights {
		sum += w
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return weights[ids[i]] > weights[ids[j]] })
	return InjectionHit{Score: 1 - math.Exp(-sum*0.75), Rules: ids}
}

func (d InjectionDetector) Scan(part int, src core.Source, text string) []core.Finding {
	if len(text) < 12 {
		return nil
	}
	h := ScoreInjection(text)
	th := d.Threshold
	if th == 0 {
		th = 0.45
	}
	if h.Score < th {
		return nil
	}
	sev := h.Score
	if src == core.SourceUser && !actionRequest(h.Rules) {
		sev *= 0.85
	}
	if src == core.SourceToolDesc {
		sev = math.Min(1, sev*1.1)
	}
	top := h.Rules
	if len(top) > 4 {
		top = top[:4]
	}
	return []core.Finding{{
		Detector:   "injection_rules",
		Tier:       0,
		Category:   core.CatInjection,
		Rule:       strings.Join(top, ","),
		Severity:   sev,
		Confidence: math.Min(0.95, 0.5+h.Score/2),
		Message:    "prompt injection pattern in " + string(src) + ": " + strings.Join(top, ", "),
		Span:       &core.Span{Part: part, Start: 0, End: len(text)},
	}}
}

func actionRequest(rules []string) bool {
	for _, id := range rules {
		for _, p := range []string{"exfil.", "priv.", "code."} {
			if strings.HasPrefix(id, p) {
				return true
			}
		}
	}
	return false
}
