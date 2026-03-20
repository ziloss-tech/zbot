package search

import (
	"github.com/ziloss-tech/zbot/internal/agent"
)

// Skill provides web search and page scraping capabilities.
type Skill struct{}

func NewSkill() *Skill { return &Skill{} }

func (s *Skill) Name() string        { return "search" }
func (s *Skill) Description() string { return "Web search via DuckDuckGo and page scraping" }

func (s *Skill) Tools() []agent.Tool {
	return []agent.Tool{
		&WebSearchTool{},
		&ScrapePageTool{},
	}
}

func (s *Skill) SystemPromptAddendum() string {
	return `### Web Search & Scraping
You can research the internet using:
- web_search: Search DuckDuckGo for any topic, returns titles, snippets, and URLs
- scrape_page: Fetch and extract readable text from any URL

Research workflow: use web_search first to find relevant URLs, then scrape_page to get full content.`
}
