package skills

// Skill describes a command that agents can invoke.
type Skill struct {
	Command     string
	Description string
	Usage       string
}

// AllSkills returns the list of ragtime skills available to agents.
func AllSkills() []Skill {
	return []Skill{
		{
			Command:     "rt search <collection> <query>",
			Description: "Search indexed documents for relevant context",
			Usage:       "rt search project-docs \"how does auth work\"",
		},
		{
			Command:     "rt search --collections",
			Description: "List available RAG collections",
			Usage:       "rt search --collections",
		},
		{
			Command:     "rt add <collection> <content>",
			Description: "Add knowledge to a RAG collection",
			Usage:       "rt add project-notes \"The auth module uses JWT tokens\"",
		},
		{
			Command:     "rt index list",
			Description: "List all indexed collections with stats",
			Usage:       "rt index list",
		},
		{
			Command:     "rt session history",
			Description: "Review recent session activity",
			Usage:       "rt session history --last 10",
		},
		{
			Command:     "rt session note <text>",
			Description: "Save a note for cross-session context",
			Usage:       "rt session note \"Decided to use strategy pattern for providers\"",
		},
	}
}
