package github

import (
	"github.com/ziloss-tech/zbot/internal/agent"
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
func (s *Skill) Description() string { return "GitHub — repos, issues, PRs, code search, commits" }

func (s *Skill) Tools() []agent.Tool {
	return []agent.Tool{
		// v1 tools
		&ListIssuesTool{client: s.client},
		&GetIssueTool{client: s.client},
		&CreateIssueTool{client: s.client},
		&ListPRsTool{client: s.client},
		&GetFileTool{client: s.client},
		// v2 tools
		&SearchReposTool{client: s.client},
		&SearchCodeTool{client: s.client},
		&ListCommitsTool{client: s.client},
		&CreatePRTool{client: s.client},
		&ListBranchesTool{client: s.client},
		&ListTreeTool{client: s.client},
		&CommentIssueTool{client: s.client},
		&GetRepoTool{client: s.client},
	}
}

func (s *Skill) SystemPromptAddendum() string {
	return `### GitHub (13 tools)
You have full access to GitHub repos. If the user doesn't specify a repo, use the default.
- **Search**: github_search_repos, github_search_code
- **Repos**: github_get_repo, github_list_tree, github_list_branches
- **Issues**: github_list_issues, github_get_issue, github_create_issue, github_comment_issue
- **PRs**: github_list_prs, github_create_pr
- **Code**: github_get_file, github_list_commits`
}
