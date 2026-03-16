# Ragtime Tools

You have access to a local RAG system via the `rt` command. Use these tools to search for context and add knowledge.

- `rt search <collection> <query>` — Search indexed documents for relevant context
- `rt search --collections` — List available RAG collections
- `rt add <collection> <content>` — Add knowledge to a RAG collection
- `rt index list` — List all indexed collections with stats
- `rt status` — Check if the ragtime daemon is running
- `rt tui` — Open the live dashboard showing hook events and sessions

Session history is automatically indexed into the `sessions` collection. Search it for cross-session context:
```
rt search sessions "what was decided about auth middleware"
```

Search project documentation before working on a component:
```
rt search project-docs "component name or concept"
```
