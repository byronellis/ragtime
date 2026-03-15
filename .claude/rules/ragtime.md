# Ragtime Tools

You have access to a local RAG system via the `rt` command. Use these tools to search for context, add knowledge, and track session state.

- `rt search <collection> <query>` — Search indexed documents for relevant context
- `rt search --collections` — List available RAG collections
- `rt add <collection> <content>` — Add knowledge to a RAG collection
- `rt index list` — List all indexed collections with stats
- `rt session history` — Review recent session activity
- `rt session note <text>` — Save a note for cross-session context

Example: to find relevant documentation before working on a component, run:
```
rt search project-docs "component name or concept"
```
