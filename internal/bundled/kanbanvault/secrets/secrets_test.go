package secrets

import (
	"slices"
	"strings"
	"testing"
	"time"
)

// Fixture bodies, assembled into tokens at run time so no complete
// credential-shaped literal sits in this file for a secret scanner to find.
const (
	body = "aB3dEf7gHi2jKlMnOpQrStUvWx0123456789xYzAbCdEfGh"
	hex  = "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6"
)

func b(n int) string { return body[:n] }

// pemBlock builds PEM armour at run time, for the same reason.
func pemBlock(label, content string) string {
	dashes := strings.Repeat("-", 5)
	return dashes + "BEGIN " + label + dashes + "\n" + content + "\n" + dashes + "END " + label + dashes
}

func redact(text string, patterns ...string) (string, int) {
	return Redact(text, Options{CustomPatterns: CompilePatterns(patterns)})
}

func TestRedactsBuiltinProviderAndCloudKeys(t *testing.T) {
	cases := []struct{ label, input string }{
		{"an AWS access key id", "creds AKIAIOSFODNN7EXAMPLE here"},
		{"a GitHub token", "token ghp_" + strings.Repeat("a", 36) + " here"},
		{"an OpenAI project key", "key sk-proj-aB3dEf_gHi-jKlMnOpQrStUvWx0123456789xY here"},
		{"an Anthropic key", "key sk-ant-api03-aB3dEf_gHi-jKlMnOpQrStUvWx0123456789 here"},
		{"a LangWatch key", "key sk-lw-aB3dEf_gHi-jKlMnOpQrStUvWx0123456789 here"},
		{"a Slack token", "xoxb-" + strings.Repeat("1", 20) + " here"},
		{"a Google API key", "AIza" + strings.Repeat("A", 35) + " here"},
		{"a Stripe secret key", "sk_live_" + strings.Repeat("a", 24) + " here"},
		{"a JWT", "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.abcDEF123456"},
	}
	for _, c := range cases {
		text, n := redact(c.input)
		if !strings.Contains(text, Marker) || n < 1 {
			t.Errorf("%s: got %q (%d)", c.label, text, n)
		}
	}
}

func TestRedactsAWholePEMBlock(t *testing.T) {
	text, _ := redact("key:\n" + pemBlock("RSA PRIVATE KEY", "MIIabc\nDEFghi") + "\ntail")
	if strings.Contains(text, "MIIabc") || !strings.Contains(text, Marker) || !strings.Contains(text, "tail") {
		t.Errorf("got %q", text)
	}
}

func TestConnectionURLs(t *testing.T) {
	cases := [][2]string{
		{"postgres://app:hunter2@db.internal:5432/app", "postgres://app:[SECRET]@db.internal:5432/app"},
		{"redis://default:Ab3xY9zQ@cache-01:6379", "redis://default:[SECRET]@cache-01:6379"},
		{"mongodb+srv://admin:p%40ss@cluster0.mongodb.net", "mongodb+srv://admin:[SECRET]@cluster0.mongodb.net"},
		{"git+ssh://git:token123@github.com/langwatch/langwatch.git", "git+ssh://git:[SECRET]@github.com/langwatch/langwatch.git"},
		{"jdbc:postgresql://svc:9f8e7d6c@10.0.0.4:5432/app", "jdbc:postgresql://svc:[SECRET]@10.0.0.4:5432/app"},
		{"HTTPS://USER:PASS@EXAMPLE.COM", "HTTPS://USER:[SECRET]@EXAMPLE.COM"},
	}
	for _, c := range cases {
		if got, _ := redact(c[0]); got != c[1] {
			t.Errorf("%q: got %q, want %q", c[0], got, c[1])
		}
	}
	for _, in := range []string{
		"://user:password@host",
		"no scheme here: user:password@host",
		"See https://app.langwatch.ai/api/trace/abc123?include=spans",
		"mail someone@example.com and read @langwatch/redaction",
	} {
		if got, _ := redact(in); got != in {
			t.Errorf("%q changed to %q", in, got)
		}
	}
}

func TestBearerKeepsItsPrefix(t *testing.T) {
	if got, _ := redact("Authorization: Bearer abc123token456xyz"); got != "Authorization: Bearer [SECRET]" {
		t.Errorf("got %q", got)
	}
}

func TestOrdinaryTextIsUnchanged(t *testing.T) {
	for _, in := range []string{
		"The model answered in 42 ms and the user said thanks.",
		"please ask the desk about the task",
	} {
		if got, n := redact(in); got != in || n != 0 {
			t.Errorf("%q: got %q (%d)", in, got, n)
		}
	}
}

func TestPastTheScanBudget(t *testing.T) {
	const aws = "AKIAIOSFODNN7EXAMPLE"
	in := aws + " " + strings.Repeat("x", maxScanLength+1)
	text, n := redact(in)
	if strings.Contains(text, aws) || n != 1 || len(text) != len(in)-len(aws)+len(Marker) {
		t.Errorf("first slice: %d redactions, len %d", n, len(text))
	}

	text, n = redact(strings.Repeat("x", maxScanLength+10_000) + " " + aws + " tail")
	if strings.Contains(text, aws) || n != 1 || !strings.HasSuffix(text, " [SECRET] tail") {
		t.Errorf("past the first slice: %d redactions, tail %q", n, text[len(text)-20:])
	}

	for _, offset := range []int{-40, -20, -1, 0, 1, 20, 40} {
		filler := strings.Repeat("x ", (maxScanLength+offset)/2)
		text, n := redact(filler + aws + " tail")
		if strings.Contains(text, "AKIAIOSFO") || n != 1 {
			t.Errorf("offset %d: split across slices (%d redactions)", offset, n)
		}
	}

	unbroken := strings.Repeat("x", 1_000_000)
	if got, _ := redact(unbroken); got != unbroken {
		t.Error("an unbroken run changed")
	}

	pem := pemBlock("PRIVATE KEY", strings.TrimRight(strings.Repeat("MIIEvQIBADANBgkqh\n", 40), "\n"))
	text, n = redact(strings.Repeat("z ", maxScanLength/2-5) + pem + " tail")
	if strings.Contains(text, "MIIEvQIBADANBgkqh") || n != 1 {
		t.Errorf("PEM across a boundary: %d redactions", n)
	}
}

func TestModernBase64URLProviderKey(t *testing.T) {
	key := "sk-proj-aB3dEf_gHi-jKlMnOpQrStUvWx0123456789xYaB-cD_eF"
	if got, _ := redact("here is my key " + key + " thanks"); got != "here is my key [SECRET] thanks" {
		t.Errorf("got %q", got)
	}
}

func TestCustomPatterns(t *testing.T) {
	json := `{"api_key":"sk-proj-abc123def456","model":"gpt-5-mini"}`
	if got, n := redact(json, "sk-.*"); got != `{"api_key":"[SECRET]","model":"gpt-5-mini"}` || n != 1 {
		t.Errorf("greedy pattern in JSON: %q (%d)", got, n)
	}
	if got, n := redact("token acme_live_abcd1234 end", "acme_live_[a-z0-9]{8,}"); got != "token [SECRET] end" || n != 1 {
		t.Errorf("company token: %q (%d)", got, n)
	}
}

func TestBeyondTheKnownVendorList(t *testing.T) {
	prompt := "I had to kill the other exploring agent, key now: zyq_8fK2mQ7pXvL4nR9sT1wZ3yB6cD0eG5hJ2kM4pQ7rS9t"
	if got, n := redact(prompt); got != "I had to kill the other exploring agent, key now: [SECRET]" || n != 1 {
		t.Errorf("unknown vendor in a sentence: %q (%d)", got, n)
	}
	if got, _ := redact("zyq_8fK2mQ7pXvL4nR9sT1wZ3yB6cD0eG5hJ2kM4pQ7rS9t"); got != Marker {
		t.Errorf("unknown vendor alone: %q", got)
	}
}

func TestWidelyUsedVendors(t *testing.T) {
	keys := []struct{ vendor, key string }{
		{"GitLab", "GITLAB_TOKEN=glpat-" + b(21)},
		{"npm", "npm_" + b(36)},
		{"Docker Hub", "dckr_pat_" + b(28)},
		{"Shopify", "shpat_" + hex},
		{"SendGrid", "SG." + b(22) + "." + b(43)},
		{"Hugging Face", "hf_" + b(34)},
		{"Groq", "gsk_" + b(47)},
		{"Perplexity", "pplx-" + b(40)},
		{"Replicate", "r8_" + b(37)},
		{"xAI", "xai-" + b(47)},
		{"Notion", "ntn_" + b(40)},
		{"DigitalOcean", "dop_v1_" + hex + hex},
		{"Figma", "figd_" + b(36)},
		{"Square", "sq0atp-" + b(22)},
		{"Mailgun", "key-" + hex},
		{"Resend", "re_" + b(24)},
		{"PostHog", "phx_" + b(36)},
		{"Linear", "lin_api_" + b(40)},
		{"Google OAuth", "ya29." + b(28)},
		{"Supabase", "sbp_" + hex + hex[:8]},
		{"Telegram", "123456789:AA" + b(33)},
		{"Airtable", "pat" + b(14) + "." + hex + hex},
	}
	for _, k := range keys {
		if _, n := redact(k.key); n == 0 {
			t.Errorf("%s survived", k.vendor)
		}
	}
}

func TestLangWatchKeys(t *testing.T) {
	for _, k := range []struct{ kind, key string }{
		{"API key", "sk-lw-" + b(12) + "_" + b(32)},
		{"ingest key", "ik-lw-" + b(12) + "_" + b(32)},
		{"legacy personal access token", "pat-lw-" + b(12) + "_" + body},
		{"a short API key", "sk-lw-123af"},
		{"a short ingest key", "ik-lw-123af"},
		{"a short legacy token", "pat-lw-123af"},
	} {
		if _, n := redact(k.key); n == 0 {
			t.Errorf("%s survived", k.kind)
		}
	}
}

func TestPrefixedAllHexKeys(t *testing.T) {
	for _, in := range []string{"acme_live_" + hex, "widget_test_" + hex, "store_secret_" + hex} {
		if got, _ := redact(in); got != Marker {
			t.Errorf("%q: got %q", in, got)
		}
	}
	commit := "commit_key_51d07b547d0a8f3e2c1b9d4a6e7f8091a2b3c4d5"
	if got, _ := redact(commit); got != commit {
		t.Errorf("commit id: got %q", got)
	}
	if _, n := redact("trace_token_" + hex + "aabbccdd"); n != 0 {
		t.Error("trace id redacted")
	}
}

func TestCredentialsTheVendorListHadMissed(t *testing.T) {
	missed := []struct{ label, value string }{
		{"Google OAuth client secret", "GOCSPX-" + b(24)},
		{"LangWatch virtual key", "vk-lw-" + b(12) + "_" + b(32)},
		{"a short LangWatch virtual key", "vk-lw-123af"},
		{"Metabase key", "mb_" + b(44)},
		{"an Authorization Token header", "Authorization: Token " + b(32)},
		{"an Okta SSWS Authorization header", "Authorization: SSWS " + b(32)},
		{"an Opsgenie GenieKey header", "Authorization: GenieKey " + b(32)},
		{"a Splunk header", "Authorization: Splunk " + b(32)},
		{"an OAuth header", "Authorization: OAuth " + b(32)},
		{"an encrypted PEM block", pemBlock("ENCRYPTED PRIVATE KEY", "MIIabc")},
		{"a PGP private key block", pemBlock("PGP PRIVATE KEY BLOCK", "lQOYBF")},
		{"a PuTTY private key", "PuTTY-User-Key-File-3: ssh-rsa\nPrivate-Lines: 8\nAAAABBBB\n\ntail"},
		{"an embedded kubeconfig key", "client-key-data: " + b(44)},
		{"an embedded kubeconfig certificate", "client-certificate-data: " + b(44)},
	}
	for _, m := range missed {
		if _, n := redact(m.value); n == 0 {
			t.Errorf("%s survived", m.label)
		}
	}
	for _, in := range []string{"phx_" + b(36), "Bearer " + b(32), pemBlock("PRIVATE KEY", "MIIabc")} {
		if _, n := redact(in); n != 1 {
			t.Errorf("%q: %d redactions", in, n)
		}
	}
}

func TestStandardBase64Body(t *testing.T) {
	for _, in := range []string{"acme_aB3dEf+gHi/jKlMnOpQrStUvWx0123456789xY", "acme_aB+dEf/gHi+jKlMnOpQrStUvWx0123456789xY"} {
		if got, _ := redact(in); got != Marker {
			t.Errorf("%q: got %q", in, got)
		}
	}
}

func TestUpperAndMixedCasePrefixes(t *testing.T) {
	for _, in := range []string{"LW_" + b(43), "Xy_" + b(38)} {
		if got, _ := redact(in); got != Marker {
			t.Errorf("%q: got %q", in, got)
		}
	}
	for _, name := range []string{"AWS_SECRET_ACCESS_KEY", "DATABASE_URL_PRODUCTION", "LANGWATCH_TELEMETRY_ENDPOINT_OVERRIDE_URL", "SHA512-" + b(43)} {
		if got, _ := redact(name); got != name {
			t.Errorf("%q changed to %q", name, got)
		}
	}
}

func TestCredentialNamedInProse(t *testing.T) {
	cases := [][2]string{
		{"my api key: h9Kd2Lm4Nq7Pr1Ts5Vw8Xz3", "my api key: [SECRET]"},
		{"TWILIO_AUTH_TOKEN=a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6", "TWILIO_AUTH_TOKEN=[SECRET]"},
		{`{"client_secret":"h9Kd2Lm4Nq7Pr1Ts5Vw8Xz3"}`, `{"client_secret":"[SECRET]"}`},
		{"Authorization: Basic dXNlcjpzdXBlcnNlY3JldDEyMw==", "Authorization: Basic [SECRET]"},
	}
	for _, c := range cases {
		if got, _ := redact(c[0]); got != c[1] {
			t.Errorf("%q: got %q, want %q", c[0], got, c[1])
		}
	}
}

// Text that only looks like secrets: over-redaction is a bug of the same
// severity as a leak. Every string here genuinely occurs.
var leaveAlone = []struct{ label, input string }{
	{"a commit hash", "fix in commit 51d07b547d0a8f3e2c1b9d4a6e7f8091a2b3c4d5"},
	{"short commit hashes", "reverted 5ebf89d6f4 and f05d495818"},
	{"a UUID", "id 550e8400-e29b-41d4-a716-446655440000 done"},
	{"an uppercase UUID", "ID 550E8400-E29B-41D4-A716-446655440000 done"},
	{"trace and span ids", "traceparent: 00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"},
	{"ISO timestamps", "started 2026-08-10T14:32:11.482Z ended 14:32:19.005Z"},
	{"a source path", "see modules/metric/process/src/services/metric-request-collection.service.ts"},
	{"a path with a line number", "packages/redaction/src/secrets.ts:142"},
	{"a URL with query parameters", "https://app.langwatch.ai/project/my-project/traces?spanId=abc123"},
	{"a base64 data URI", "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="},
	{"model names", "compared claude-opus-5 with gpt-5-mini and claude-3-5-sonnet-20241022"},
	{"a bedrock model id", "us.anthropic.claude-opus-4-20250514-v1:0"},
	{"semver bumps", "bumped langwatch from 1.2.1 to 2.6.0 and web to 3.9.0"},
	{"a subresource integrity hash", "integrity sha512-4Zj6ZL6qF9pQwEr7tYu2Io1pAsDfGh3JkL5mNb8Vc9X"},
	{"a docker image digest", "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"},
	{"a content-hashed asset", "dist/assets/index-DxK9mQ2p.js 1,234.56 kB"},
	{"a kubernetes pod name", "pod/langwatch-app-7d9f8c6b5d-x2mnq restarted"},
	{"an AWS ARN", "arn:aws:iam::123456789012:role/langwatch-app-runtime-role"},
	{"a snake_case identifier", "const user_id_1234567890abcdef = row.id;"},
	{"reading an environment variable", "const apiKey = process.env.OPENAI_API_KEY;"},
	{"a property access chain", "const token = config.auth.accessToken.value;"},
	{"a path following a credential keyword", "token: src/server/app-layer/traces/log-request-collection.service.ts"},
	{"an absolute path", "secret = /etc/langwatch/credentials.yaml"},
	{"an environment variable reference", "export ACME_API_KEY=$ACME_API_KEY"},
	{"a documented placeholder", "OPENAI_API_KEY=your-api-key-here"},
	{"a masked value", "api_key: xxxxxxxxxxxxxxxxxxxx"},
	{"a documented header", "Send the Authorization: Basic <base64-credentials> header."},
	{"a sentence about basic auth", "This uses Basic authentication over TLS."},
	{"a documented bearer header", "Send the Authorization: Bearer <your-token> header."},
	{"prose using the word key", "The key insight is that the token budget was the bottleneck."},
	{"advice about a key", "Set the api key in your .env file before running the tests"},
	{"an API version field", `{"api_version": "2024-10-21", "model": "claude-opus-5"}`},
	{"a request id header", "X-Request-Id: 7f3a9b2c-1d4e-5f6a-8b9c-0d1e2f3a4b5c"},
	{"usage attributes", "gen_ai.usage.input_tokens=15234 gen_ai.usage.output_tokens=892"},
	{"a branch name", "feat/coding-agent-session-events and issue6124/red-team-native"},
	{"already-redacted text", "key now: [SECRET] and token: [SECRET]"},
	{"an ordinary agent sentence", "I had to kill the other exploring agent because it was burning tokens on the same files."},
	// Bare high-entropy tokens with no prefix: a bare 32-hex credential and
	// a trace id are byte for byte the same shape.
	{"a bare 32-hex token", hex},
	{"a bare 64-hex token", hex + hex},
	{"a bare base62 token", body},
	{"a bare 21-character nanoid", "V1StGXR8Z5jdHi6BmyT"},
	{"a bare 32-character nanoid", "V1StGXR8Z5jdHi6BmyTV1StGXR8Z5jdH"},
	{"a bare git SHA", "51d07b547d0a8f3e2c1b9d4a6e7f8091a2b3c4d5"},
	{"a bare trace id", "4bf92f3577b34da6a3ce929d0e0e4736"},
	{"a bare span id", "00f067aa0ba902b7"},
	// Environment variable names.
	{"an AWS environment variable name", "AWS_SECRET_ACCESS_KEY"},
	{"a database environment variable name", "DATABASE_URL_PRODUCTION"},
	{"a long environment variable name", "LANGWATCH_TELEMETRY_ENDPOINT_OVERRIDE_URL"},
	// Identifier prefixes in front of a hex body.
	{"a prefixed commit id", "commit_51d07b547d0a8f3e2c1b9d4a6e7f8091a2b3c4d5"},
	{"a prefixed trace id", "trace_4bf92f3577b34da6a3ce929d0e0e4736"},
	{"a prefixed digest", "digest_" + hex},
	{"an identifier prefix carrying a credential segment", "commit_key_51d07b547d0a8f3e2c1b9d4a6e7f8091a2b3c4d5"},
	{"a trace prefix carrying a credential segment", "trace_token_" + hex + "aabbccdd"},
	// Ordinary words containing a vendor prefix.
	{"words containing sk- and ask-", "risk-based scoring, disk-usage report, ask-me-anything, mask-sensitive-fields"},
	{"a transcript tag", "<task-notification> and <task-progress>"},
	{"package versions", "@langwatch/gateway-process@3.12.0 and pnpm@10.4.1"},
	// A public PostHog project key ships in web bundles by design.
	{"a public PostHog project key", "phc_" + b(43)},
	// Record ids, minted in the same shape as a key.
	{"a project id", "project_" + b(29)},
	{"a card id", "card_" + b(29)},
	{"a scenario id", "scenario_" + b(29)},
	{"a langy conversation id", "langyconv_" + b(29)},
	{"a provider id", "provider_" + b(29)},
	{"an OpenAI completion id", "chatcmpl-" + b(29)},
	{"an Anthropic tool use id", "toolu_" + b(29)},
	{"a thread id", "thread_" + b(29)},
	{"a run id", "run_" + b(29)},
	{"an assistant id", "asst_" + b(29)},
	// Prose after a credential word.
	{"prose about a digest of a key", "a bare digest of an API key is offline-checkable against a list"},
	{"prose about an authorization header", "the Authorization header is attacker-controlled and can be killed"},
	// A bare key field names a map entry, not a credential.
	{"a key field holding a content hash", `{"key":"` + strings.Repeat("a1b2c3d4e5f6", 3) + `"}`},
	{"a key field holding a UUID", `{"key":"550e8400-e29b-41d4-a716-446655440000"}`},
	{"a key field holding a record id", `{"key":"project_` + b(29) + `"}`},
	{"a key entry in an OTLP attribute pair", `{"key":"gen_ai.usage.input_tokens"}`},
	{"a key field in YAML holding a commit", "key: " + strings.Repeat("9f8e7d6c5b4a3928", 2)},
}

func TestTextThatOnlyLooksLikeSecrets(t *testing.T) {
	for _, c := range leaveAlone {
		if got, _ := redact(c.input); got != c.input {
			t.Errorf("%s: %q", c.label, got)
		}
		if found := Find(c.input); len(found) > 0 {
			t.Errorf("%s: Find reported %s", c.label, found[0].Kind)
		}
	}
}

func TestBareAndQualifiedKeyFields(t *testing.T) {
	hash := strings.Repeat("a1b2c3d4e5f6", 3)
	for _, in := range []string{`{"key":"` + hash + `"}`, "key: " + hash} {
		if got, _ := redact(in); got != in {
			t.Errorf("%q changed to %q", in, got)
		}
	}
	for _, name := range []string{"api_key", "secret_key", "apiKey", "x-api-key"} {
		if got, _ := redact(name + ": " + hash); got != name+": [SECRET]" {
			t.Errorf("%s: got %q", name, got)
		}
	}
	for _, in := range []string{"OPENAI_API_KEY=your-api-key-here", "export ACME_API_KEY=$ACME_API_KEY"} {
		if got, _ := redact(in); got != in {
			t.Errorf("%q changed to %q", in, got)
		}
	}
}

func TestLargePayloadWithinBudget(t *testing.T) {
	chunk := "The agent read modules/trace/process/src/services/trace-legacy-read.service.ts at " +
		"2026-08-10T14:32:11.482Z, commit 51d07b547d0a8f3e2c1b9d4a6e7f8091a2b3c4d5, " +
		`model claude-opus-5, {"input_tokens":15234,"cost_usd":0.0412}` + "\n"
	payload := strings.Repeat(chunk, 200_000/len(chunk)+1)[:200_000]
	started := time.Now()
	if _, n := redact(payload); n != 0 {
		t.Errorf("%d redactions in ordinary text", n)
	}
	if d := time.Since(started); d > 2*time.Second {
		t.Errorf("took %s", d)
	}
}

func TestStaysLinearOnAdversarialInput(t *testing.T) {
	for _, c := range []struct{ label, input string }{
		{"a long underscore run", strings.Repeat("a_", 50_000)},
		{"a long hyphen run", strings.Repeat("a-", 50_000)},
		{"a keyword followed by filler", "api_key: " + strings.Repeat("a", 100_000)},
		{"repeated keywords", strings.Repeat("password=", 20_000)},
		{"repeated open quotes", strings.Repeat(`api_key:"`, 20_000) + "x"},
		{"lowercase prose with no URL in it", strings.Repeat("the dashboard stopped loading ", 8_000)},
		{"one URL per line", strings.Repeat("postgres://svc:pw@10.0.0.4:5432/app\n", 6_000)},
		{"a scheme-shaped run with no separator", strings.Repeat("a", 100_000) + "://"},
	} {
		in := c.input
		if len(in) > maxScanLength {
			in = in[:maxScanLength]
		}
		started := time.Now()
		redact(in)
		if d := time.Since(started); d > 2*time.Second {
			t.Errorf("%s took %s", c.label, d)
		}
	}
}

func TestHandWrittenCustomPattern(t *testing.T) {
	transcript := []string{
		"<task-notification>",
		"</task-notification>",
		"<task-notification>agent finished</task-notification> and then it stopped",
		"<system-reminder>The user has not replied yet.</system-reminder>",
		"TaskCreate returned a new agent id",
		"we took a risk-based approach to the migration",
		"disk-usage is at 84% on the clickhouse node",
		"ask-follow-up was disabled for this run",
		"the mask-sensitive-fields flag is on",
		"flask-restful is not a dependency here",
		"moved the desk-check to the review step",
	}
	for _, line := range transcript {
		if got, _ := redact(line, "sk-.*"); got != line {
			t.Errorf("custom pattern changed %q to %q", line, got)
		}
		if got, _ := redact(line); got != line {
			t.Errorf("built-in rules changed %q to %q", line, got)
		}
	}
	if got, _ := redact("the key is sk-proj-abc123def456 and the model is gpt-5-mini", "sk-.*"); got != "the key is [SECRET] and the model is gpt-5-mini" {
		t.Errorf("trailing wildcard: %q", got)
	}
	if got, n := redact(`{"api_key":"sk-proj-abc123def456","model":"gpt-5-mini"}`, "sk-.*"); got != `{"api_key":"[SECRET]","model":"gpt-5-mini"}` || n != 1 {
		t.Errorf("closing quote: %q (%d)", got, n)
	}
	if got, _ := redact("sk-notarealprovider-abc123def456", "sk-.*"); got != Marker {
		t.Errorf("its own credential: %q", got)
	}
	if got, _ := redact("token acme_abcd1234 end", `\bacme_[a-z0-9]{8,}`); got != "token [SECRET] end" {
		t.Errorf("self-anchored: %q", got)
	}
}

func TestOverBroadProbe(t *testing.T) {
	for _, p := range []string{".*", `\w+`, "[a-z]+", `[\s\S]*`} {
		if OverBroadProbe(p) == "" {
			t.Errorf("%q not reported as too broad", p)
		}
	}
	for _, p := range []string{"acme_live_[a-z0-9]{8,}", "sk-[A-Za-z0-9]{20,}", `\bzyq_[A-Za-z0-9]{40}`, "sk-.*"} {
		if got := OverBroadProbe(p); got != "" {
			t.Errorf("%q reported as too broad on %q", p, got)
		}
	}
	for _, p := range []string{"[unclosed", "", "   "} {
		if got := OverBroadProbe(p); got != "" {
			t.Errorf("%q: %q", p, got)
		}
	}
	// A named group opens like a lookbehind and still gets the guard.
	if got := OverBroadProbe("(?<key>sk-.*)"); got != "" {
		t.Errorf("named group: %q", got)
	}
	if got, _ := redact("a <task-notification> here", "(?<key>sk-.*)"); got != "a <task-notification> here" {
		t.Errorf("named group redacted %q", got)
	}
	// RE2 has no lookbehind, so a pattern that uses one does not compile
	// and is left to the caller's own compile check.
	if got := OverBroadProbe("(?<=the )user"); got != "" {
		t.Errorf("lookbehind: %q", got)
	}
}

func TestSensitiveAttributeKeys(t *testing.T) {
	for _, k := range []string{"signingSecret", "bearerToken", "webhookSecret", "masterKey", "encryptionKey",
		"SecretAccessKey", "AccessKeyId", "SecretString", "SecretBinary", "AuthorizationToken", "PasswordHash",
		"verificationToken", "rootPassword", "credentialsJson",
		"Authorization", "x-api-key", "DB_PASSWORD", "client_secret", "set-cookie"} {
		if !IsSensitiveAttributeKey(k) {
			t.Errorf("%s not recognised", k)
		}
	}
	for _, k := range []string{"idempotency_key", "partition_key", "cacheKey", "sortKey", "primaryKey",
		"gen_ai.usage.input_tokens", "tokenCount", "keyboardLayout", "monkeyPatch",
		"model", "latency", "span.name"} {
		if IsSensitiveAttributeKey(k) {
			t.Errorf("%s flagged", k)
		}
	}
}

func TestWhitespaceSeparator(t *testing.T) {
	if _, n := redact("Authorization " + b(30)); n != 1 {
		t.Errorf("Authorization <token>: %d", n)
	}
	if got, _ := redact("api key " + hex); got != "api key [SECRET]" {
		t.Errorf("api key <hex>: %q", got)
	}
	for _, line := range []string{
		"a bare digest of an API key is offline-checkable against a list",
		"the Authorization header is attacker-controlled and can be killed",
		"the secret sauce here is careful measurement of everything",
		"your password should be long and memorable and never reused",
		`api_key = $OPENAI_API_KEY\nnext line here`,
		"api_key = $OPENAI_API_KEY\nnext line here",
		"github_token_secret = local.github_token_secret_name",
	} {
		if got, _ := redact(line); got != line {
			t.Errorf("%q changed to %q", line, got)
		}
	}
}

func TestSkipList(t *testing.T) {
	const runID = "unlisted_0005FFcHZ7IBvPE1OSWymml0ikKqB"
	skip := Options{SkipRuleIDs: ShapeOnlyRuleIDs}
	if got, _ := redact(runID); got != Marker {
		t.Errorf("without a skip list: %q", got)
	}
	if got, _ := Redact(runID, skip); got != runID {
		t.Errorf("with a skip list: %q", got)
	}
	if got, _ := Redact("sk-ant-api03-"+body, skip); got != Marker {
		t.Errorf("other rules: %q", got)
	}
	if got, _ := Redact(runID, Options{SkipRuleIDs: ShapeOnlyRuleIDs, CustomPatterns: CompilePatterns([]string{"unlisted_[A-Za-z0-9]+"})}); got != Marker {
		t.Errorf("custom patterns: %q", got)
	}
	if got, _ := Redact(strings.Repeat("x", maxScanLength)+" "+runID, skip); !strings.Contains(got, runID) {
		t.Error("over the budget the skip list was lost")
	}
	if len(Detect(runID, Options{})) != 1 || len(Detect(runID, skip)) != 0 {
		t.Error("Detect ignores the skip list")
	}
}

func TestCompilePatternsSkipsUncompilable(t *testing.T) {
	if n := len(CompilePatterns([]string{"valid[0-9]+", "("})); n != 1 {
		t.Errorf("compiled %d", n)
	}
}

func TestBuiltinRules(t *testing.T) {
	rules := BuiltinRules()
	ids := make([]string, len(rules))
	for i, r := range rules {
		if r.ID == "" || r.Description == "" {
			t.Errorf("rule %d has no id or description", i)
		}
		ids[i] = r.ID
	}
	if len(rules) < 8 {
		t.Errorf("%d rules", len(rules))
	}
	for _, id := range append([]string{"vendor_api_key", "shaped_api_key", "sensitive_assignment"}, ShapeOnlyRuleIDs...) {
		if !slices.Contains(ids, id) {
			t.Errorf("no rule %s", id)
		}
	}
}

func ruleIDs(ms []Match) []string {
	var ids []string
	for _, m := range ms {
		ids = append(ids, m.RuleID)
	}
	slices.Sort(ids)
	return ids
}

func TestDetect(t *testing.T) {
	in := "key sk-proj-aB3dEf_gHi-jKlMnOpQrStUvWx0123456789xY here"
	if ms := Detect(in, Options{}); len(ms) != 1 || ms[0].RuleID != "provider_api_key" {
		t.Errorf("provider key: %v", ms)
	}

	in = "db postgres://user:hunter2@db.internal:5432/app end"
	ms := Detect(in, Options{})
	if len(ms) != 1 || ms[0].RuleID != "url_credentials" || in[ms[0].Start:ms[0].End] != "postgres://user:hunter2@" {
		t.Errorf("connection URL: %v", ms)
	}
	in = "jdbc:postgresql://svc:9f8e7d6c@10.0.0.4:5432/app"
	if ms := Detect(in, Options{}); len(ms) == 0 || in[ms[0].Start:ms[0].End] != "postgresql://svc:9f8e7d6c@" {
		t.Errorf("jdbc URL: %v", ms)
	}

	got := ruleIDs(Detect("aws AKIAIOSFODNN7EXAMPLE and gh ghp_"+strings.Repeat("a", 36), Options{}))
	if !slices.Equal(got, []string{"aws_access_key_id", "github_token"}) {
		t.Errorf("several secrets: %v", got)
	}

	custom := Options{CustomPatterns: CompilePatterns([]string{"acme_live_[a-z0-9]{8,}"})}
	if ms := Detect("token acme_live_abcd1234 end", custom); len(ms) != 1 || ms[0].RuleID != "custom_pattern" {
		t.Errorf("custom: %v", ms)
	}

	if ms := Detect("the user said thanks", Options{}); len(ms) != 0 {
		t.Errorf("ordinary text: %v", ms)
	}
	if ms := Detect("authorization: [SECRET]", Options{}); len(ms) != 0 {
		t.Errorf("marker: %v", ms)
	}

	if ms := Detect("api_key: sk-proj-aB3dEf_gHi-jKlMnOpQrStUvWx0123456789xY", Options{}); len(ms) != 1 || ms[0].RuleID != "provider_api_key" {
		t.Errorf("one credential, several rules: %v", ms)
	}
	if ms := Detect("key now: zyq_8fK2mQ7pXvL4nR9sT1wZ3yB6cD0eG5hJ2kM4pQ7rS9t", Options{}); len(ms) != 1 || ms[0].RuleID != "shaped_api_key" {
		t.Errorf("unknown vendor once: %v", ms)
	}
	got = ruleIDs(Detect("aws AKIAIOSFODNN7EXAMPLE and gitlab glpat-"+b(21), Options{}))
	if !slices.Equal(got, []string{"aws_access_key_id", "vendor_api_key"}) {
		t.Errorf("two credentials: %v", got)
	}
}
