// Package chatstate owns volatile chat-first gateway correlations.
//
// The package intentionally keeps only process-local safe identifiers and
// status categories. It must not retain prompts, responses, history payloads,
// raw app-server JSONL, auth material, content hashes, or durable chat identity.
package chatstate
