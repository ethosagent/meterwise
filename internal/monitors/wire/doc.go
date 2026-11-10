// Package wire is the one place in the host that edits provider JSON (ruling R4). AppendNote splices
// a "[proxy]" note into a request body by byte offsets, so every byte outside the splice is
// unchanged and the prompt-cache prefix of the provider is not disturbed; BlockResponse builds the
// whole-call answer (JSON or SSE frames) that a blocking monitor returns instead of calling the
// upstream: one terminal, text-only assistant turn, so an agent loop ends its turn on it. It supports tollgate's openai.chat and anthropic.messages wires and imports only the
// standard library, tollgate's root and internal/contract (rule H3).
package wire
