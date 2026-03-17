// ─── SSE Events ──────────────────────────────────────────────────────────────

export interface SSEEvent {
  workflow_id: string
  task_id?: string
  source: 'planner' | 'executor' | 'critic' | 'agent'  // v2: 'agent' is the single-brain source
  type: 'token' | 'status' | 'handoff' | 'complete' | 'error' | 'reviewing' | 'verdict' | 'thinking' | 'tool_use'
  payload: string
}

// ─── API Types ───────────────────────────────────────────────────────────────

export interface PlanResponse {
  workflow_id: string
  status: string
}

export interface PlannedTask {
  id: string
  title: string
  instruction: string
  depends_on: string[]
  parallel: boolean
  tool_hints: string[]
  priority: number
}

// ─── Critic Types (DEPRECATED: kept for backwards compat with old events) ──

export type CriticVerdict = 'pass' | 'fail' | 'partial'

export interface CriticIssue {
  severity: 'critical' | 'major' | 'minor'
  description: string
  suggested_fix: string
}

export interface CriticResult {
  task_id: string
  verdict: CriticVerdict
  issues: CriticIssue[]
  corrected_instruction: string
}

// ─── Task Types ─────────────────────────────────────────────────────────────

export interface TaskDetail {
  id: string
  step: number
  name: string
  title?: string
  status: 'pending' | 'running' | 'done' | 'failed' | 'canceled'
  output: string
  error: string
  depends_on: string[]
  started_at?: string
  finished_at?: string
  duration_ms?: number
  files_changed?: string[]
  tool_calls?: ToolCallEvent[]
  // DEPRECATED: critic fields kept for old event compat
  criticStatus?: 'reviewing' | 'passed' | 'failed' | 'partial' | 'retrying'
  criticResult?: CriticResult
  outputFiles?: string[]
}

// ─── v2: Agent Streaming Types ──────────────────────────────────────────────

export interface ToolCallEvent {
  name: string
  input: string
  output?: string
  status: 'running' | 'done' | 'error'
}

export type ModelTier = 'haiku' | 'sonnet' | 'opus'

export interface AgentStreamEvent {
  type: 'thinking' | 'token' | 'tool_use' | 'tool_result' | 'complete' | 'error' | 'tier_change'
  content: string
  model_tier?: ModelTier
  tool_name?: string
}

export interface WorkflowDetail {
  id: string
  goal: string
  status: string
  tasks: TaskDetail[]
}

export interface WorkflowListItem {
  id: string
  goal: string
  status: string
  task_count: number
  done_count: number
  created_at: string
}

export interface Metrics {
  active_workflows: number
  total_tasks: number
  done_tasks: number
  tokens_today: number
  cost_today: string
}

// ─── Sprint 14: Schedule Types ──────────────────────────────────────────────

export interface ScheduledJob {
  id: string
  name: string
  goal: string
  cron_expr: string
  natural_schedule: string
  status: 'active' | 'paused' | 'running'
  next_run: string
  last_run?: string | null
  run_count: number
  created_at: string
}

// ─── Deep Research Types ─────────────────────────────────────────────────────

export interface ResearchEvent {
  session_id: string
  stage: 'planning' | 'searching' | 'extracting' | 'critiquing' | 'evaluated' | 'synthesizing' | 'complete' | 'error' | 'stream_end' | 'done'
  iteration: number
  model: string
  model_id: string
  message: string
  confidence: number
  passed: boolean
  sources: number
  claims: number
  report: string
  cost_usd: number
  error: string
  timestamp: string
}

export interface ResearchSession {
  id: string
  goal: string
  status: 'running' | 'complete' | 'failed'
  iterations: number
  confidence_score: number
  final_report: string
  cost_usd: number
  error: string
  created_at: string
  updated_at: string
}

export interface ResearchBudget {
  daily_limit_usd: number
  today_spent_usd: number
  sessions_today: number
  remaining_usd: number
}

// ─── UI State ────────────────────────────────────────────────────────────────

export type WorkflowPhase = 'idle' | 'planning' | 'handoff' | 'executing' | 'complete' | 'error'

export interface WorkflowState {
  id: string
  goal: string
  phase: WorkflowPhase
  plannerTokens: string
  plannedTasks: PlannedTask[]
  dbWorkflowID?: string
  tasks: TaskDetail[]
  error?: string
  // v2: single-brain streaming state
  agentTokens: string
  agentThinking: string
  modelTier: ModelTier
  toolCalls: ToolCallEvent[]
}

// ─── v2: Chat Message Types ─────────────────────────────────────────────────

export interface ChatMessage {
  id: string
  role: 'user' | 'assistant' | 'system'
  content: string
  timestamp: number
  modelTier?: ModelTier
  toolCalls?: ToolCallEvent[]
  thinking?: string
  isStreaming?: boolean
}
