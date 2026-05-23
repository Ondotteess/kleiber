// Package ai is the AI Bridge: it talks to LLM providers (Anthropic,
// OpenAI, Ollama) and the gopls MCP server, builds Go-aware prompts, and
// validates AI-proposed edits against the compiler before applying them.
//
// The intended public API is sketched in docs/architecture/components.md
// (section "AI Bridge"). Implementation lands in Milestone 3 (Phase 6 of
// the development plan).
package ai
