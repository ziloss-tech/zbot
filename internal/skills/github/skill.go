package github

import (
	"github.com/zbot-ai/zbot/internal/agent"
)

// Skill wraps the GitHub client and tools into a skills.Skill implementation.
type Skill struct {
	client *Client
}

// NewSkill creates a GitHub skill ready for registration.
func NewSkill(token string) *Skill {
	return &Skill{client: NewClient(token)}
}

func (s *Skill) Name() string        { return "github" }
func (s *Skill) Description() string { return "GitHub — issues, PRs, file access" }

func (s *Skill) Tools() []agent.Tool {
	return []agent.Tool{
		&ListIssuesTool{client: s.client},
		&GetIssueTool{client: s.client},
		&CreateIssueTool{client: s.client},
		&ListPRsTool{client: s.client},
		&GetFileTool{client: s.client},
	}
}

func (s *Skill) SystemPromptAddendum() string {
	return `### GitHub
You have access to GitHub repos. Default repo: your-username/zbot
- List and view issues (github_list_issues, github_get_issue)
- Create issues (github_create_issue)
- List pull requests (github_list_prs)
- Read file contents (github_get_file)`
}
