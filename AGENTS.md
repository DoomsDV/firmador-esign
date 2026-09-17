# Agent instructions

These instructions apply to Codex, Cursor, and any coding agent working in this repository.

## Context and token budget policy

Before reading or loading full files, always use the available codebase index / MCP tools first, especially `codebase-memory-mcp` when available.

Workflow:

1. Start with `codebase-memory-mcp` to understand relevant symbols, files, dependencies, references, call graph, ownership, or related modules.
2. Prefer symbol-level, function-level, class-level, or outline-level context over full file contents.
3. Only read full files when the MCP/index result is insufficient, ambiguous, or exact implementation details are required.
4. Never load broad folders, generated files, lockfiles, build outputs, snapshots, minified files, vendored code, or large config dumps unless explicitly necessary.
5. For code changes, gather only:
   - the target symbol or file,
   - direct callers/usages,
   - related tests,
   - relevant configuration,
   - nearby interfaces/types.
6. After discovering relevant context, summarize it once and reuse that summary instead of repeatedly re-reading the same files.
7. When uncertain, ask a narrower MCP/codebase query before expanding context.
8. Prefer diffs, snippets, symbol names, and file paths over pasting entire files.
9. Stop retrieving context once there is enough information to make the change safely.
10. If `codebase-memory-mcp` is unavailable in the current client, fall back to the client’s native codebase search/indexing tools before reading full files.
