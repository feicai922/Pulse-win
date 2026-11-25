package model

type BillingKind string

const (
	BillingSubscription BillingKind = "subscription"
	BillingAPI          BillingKind = "api"
)

type CredentialKind string

const (
	CredLocalLogin    CredentialKind = "localLogin"
	CredAPIKey        CredentialKind = "apiKey"
	CredAPIKeyOpt     CredentialKind = "apiKeyOptional"
	CredKeyAndAddress CredentialKind = "keyAndAddress"
	CredGitHubDevice  CredentialKind = "githubDevice"
)

type ProviderID string

const (
	ProviderClaudeCode        ProviderID = "claudeCode"
	ProviderCodex             ProviderID = "codex"
	ProviderCursor            ProviderID = "cursor"
	ProviderCopilot           ProviderID = "copilot"
	ProviderAntigravity       ProviderID = "antigravity"
	ProviderDeepSeek          ProviderID = "deepSeek"
	ProviderKimiCode          ProviderID = "kimiCode"
	ProviderZai               ProviderID = "zai"
	ProviderGLMCoding         ProviderID = "glmCoding"
	ProviderMiniMax           ProviderID = "minimax"
	ProviderOpenCodeGo        ProviderID = "openCodeGo"
	ProviderGemini            ProviderID = "gemini"
	ProviderCommandCode       ProviderID = "commandCode"
	ProviderGrok              ProviderID = "grok"
	ProviderVolcengine        ProviderID = "volcengine"
	ProviderAlibabaCodingPlan ProviderID = "alibabaCodingPlan"
	ProviderMoonshot          ProviderID = "moonshot"
	ProviderOpenAIPlatform    ProviderID = "openAIPlatform"
	ProviderKiloCode          ProviderID = "kiloCode"
	ProviderSub2API           ProviderID = "sub2api"
	ProviderNewAPI            ProviderID = "newAPI"
	ProviderKiro              ProviderID = "kiro"
)

type ProviderInfo struct {
	ID            ProviderID
	DisplayName   string
	Icon          string
	Billing       BillingKind
	Credential    CredentialKind
	BrandColor    uint32
	UsageURL      string
	HasSiteOption bool
	SupportsBalanceBasis bool
}

var BuiltInProviders = []ProviderInfo{
	{
		ID: ProviderClaudeCode, DisplayName: "Claude Code", Icon: "claude",
		Billing: BillingSubscription, Credential: CredLocalLogin, BrandColor: 0xD97757,
		UsageURL: "https://claude.ai/settings/usage",
	},
	{
		ID: ProviderCodex, DisplayName: "Codex", Icon: "openai",
		Billing: BillingSubscription, Credential: CredLocalLogin, BrandColor: 0x10A37F,
		UsageURL: "https://chatgpt.com/codex/settings/usage",
	},
	{
		ID: ProviderCursor, DisplayName: "Cursor", Icon: "cursor",
		Billing: BillingSubscription, Credential: CredLocalLogin, BrandColor: 0x000000,
		UsageURL: "https://cursor.com/dashboard?tab=usage",
	},
	{
		ID: ProviderCopilot, DisplayName: "GitHub Copilot", Icon: "github",
		Billing: BillingSubscription, Credential: CredGitHubDevice, BrandColor: 0x24292F,
		UsageURL: "https://github.com/settings/copilot",
	},
	{
		ID: ProviderAntigravity, DisplayName: "Antigravity", Icon: "antigravity",
		Billing: BillingSubscription, Credential: CredLocalLogin, BrandColor: 0x4285F4,
		UsageURL: "https://antigravity.google",
	},
	{
		ID: ProviderDeepSeek, DisplayName: "DeepSeek", Icon: "deepseek",
		Billing: BillingAPI, Credential: CredAPIKey, BrandColor: 0x4D6BFE,
		UsageURL: "https://platform.deepseek.com/usage", SupportsBalanceBasis: true,
	},
	{
		ID: ProviderKimiCode, DisplayName: "Kimi Code", Icon: "kimi",
		Billing: BillingSubscription, Credential: CredAPIKey, BrandColor: 0x000000,
		UsageURL: "https://www.kimi.com/code/console",
	},
	{
		ID: ProviderZai, DisplayName: "z.ai", Icon: "zai",
		Billing: BillingSubscription, Credential: CredAPIKey, BrandColor: 0x111111,
		UsageURL: "https://z.ai/manage-apikey/subscription",
	},
	{
		ID: ProviderGLMCoding, DisplayName: "Zhipu", Icon: "zai",
		Billing: BillingSubscription, Credential: CredAPIKeyOpt, BrandColor: 0x3859FF,
		UsageURL: "https://open.bigmodel.cn/usercenter/proj-mgmt/apikeys",
	},
	{
		ID: ProviderMiniMax, DisplayName: "MiniMax", Icon: "minimax",
		Billing: BillingSubscription, Credential: CredAPIKey, BrandColor: 0xFF4F5E,
		UsageURL: "https://platform.minimax.io/user-center/payment/coding-plan", HasSiteOption: true,
	},
	{
		ID: ProviderOpenCodeGo, DisplayName: "OpenCode Go", Icon: "opencode",
		Billing: BillingSubscription, Credential: CredAPIKeyOpt, BrandColor: 0x111111,
		UsageURL: "https://opencode.ai/zen/go",
	},
	{
		ID: ProviderGemini, DisplayName: "Gemini", Icon: "geminicli",
		Billing: BillingSubscription, Credential: CredLocalLogin, BrandColor: 0x4285F4,
		UsageURL: "https://aistudio.google.com",
	},
	{
		ID: ProviderCommandCode, DisplayName: "Command Code", Icon: "commandcode",
		Billing: BillingSubscription, Credential: CredAPIKeyOpt, BrandColor: 0x111111,
		UsageURL: "https://commandcode.ai",
	},
	{
		ID: ProviderGrok, DisplayName: "Grok", Icon: "grok",
		Billing: BillingSubscription, Credential: CredLocalLogin, BrandColor: 0x111111,
		UsageURL: "https://grok.com",
	},
	{
		ID: ProviderVolcengine, DisplayName: "Volcengine", Icon: "volcengine",
		Billing: BillingSubscription, Credential: CredAPIKeyOpt, BrandColor: 0x1664FF,
		UsageURL: "https://console.volcengine.com/ark",
	},
	{
		ID: ProviderAlibabaCodingPlan, DisplayName: "Alibaba Coding Plan", Icon: "alibabacloud",
		Billing: BillingSubscription, Credential: CredAPIKey, BrandColor: 0xFF6A00,
		UsageURL: "https://bailian.console.aliyun.com",
	},
	{
		ID: ProviderMoonshot, DisplayName: "Moonshot", Icon: "moonshot",
		Billing: BillingAPI, Credential: CredAPIKey, BrandColor: 0x111111,
		UsageURL: "https://platform.moonshot.cn/console/account", SupportsBalanceBasis: true,
	},
	{
		ID: ProviderOpenAIPlatform, DisplayName: "OpenAI API", Icon: "openai",
		Billing: BillingAPI, Credential: CredAPIKey, BrandColor: 0x10A37F,
		UsageURL: "https://platform.openai.com/settings/organization/billing/overview", SupportsBalanceBasis: true,
	},
	{
		ID: ProviderKiloCode, DisplayName: "Kilo Code", Icon: "kilocode",
		Billing: BillingSubscription, Credential: CredAPIKeyOpt, BrandColor: 0x111111,
		UsageURL: "https://app.kilo.ai", SupportsBalanceBasis: true,
	},
	{
		ID: ProviderSub2API, DisplayName: "sub2api", Icon: "sub2api",
		Billing: BillingAPI, Credential: CredKeyAndAddress, BrandColor: 0x2563EB,
		UsageURL: "", SupportsBalanceBasis: true,
	},
	{
		ID: ProviderNewAPI, DisplayName: "New API", Icon: "newapi",
		Billing: BillingAPI, Credential: CredKeyAndAddress, BrandColor: 0x0EA5E9,
		UsageURL: "", SupportsBalanceBasis: true,
	},
	{
		ID: ProviderKiro, DisplayName: "Kiro", Icon: "kiro",
		Billing: BillingSubscription, Credential: CredLocalLogin, BrandColor: 0x7C3AED,
		UsageURL: "https://kiro.dev",
	},
}

func FindProvider(id ProviderID) (ProviderInfo, bool) {
	for _, p := range BuiltInProviders {
		if p.ID == id {
			return p, true
		}
	}
	return ProviderInfo{}, false
}
