// Package router provides the adapter that implements agent.ModelRouter.
package router

import "github.com/ziloss-tech/zbot/internal/agent"

// RouterAdapter wraps the benchmark Router to implement agent.ModelRouter.
// This allows the agent core (which depends only on interfaces) to use the
// benchmark-based model selection without importing the router package directly.
type RouterAdapter struct {
	router *Router
}

// NewRouterAdapter creates a new adapter wrapping a Router.
func NewRouterAdapter(r *Router) *RouterAdapter {
	return &RouterAdapter{router: r}
}

// ClassifyTask maps a natural language task description to a task category string.
func (ra *RouterAdapter) ClassifyTask(description string) string {
	return string(ClassifyTask(description))
}

// BestModel returns the recommended model for a task category.
// preferQuality=true picks highest score; false picks best cost-efficiency.
func (ra *RouterAdapter) BestModel(category string, preferQuality bool) *agent.ModelRecommendation {
	rec := ra.router.BestModel(TaskCategory(category), preferQuality)
	if rec == nil {
		return nil
	}
	return &agent.ModelRecommendation{
		ModelID:        rec.Model.ID,
		ModelName:      rec.Model.Name,
		TaskScore:      rec.TaskScore,
		CostEfficiency: rec.CostEfficiency,
		Reason:         rec.Reason,
	}
}
