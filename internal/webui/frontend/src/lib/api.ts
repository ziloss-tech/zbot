import type { PlanResponse, WorkflowListItem, WorkflowDetail, Metrics, ScheduledJob, ResearchSession, ResearchBudget } from './types'

const BASE = ''

export async function submitPlan(goal: string): Promise<PlanResponse> {
  const res = await fetch(`${BASE}/api/plan`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ goal }),
  })
  if (!res.ok) {
    throw new Error(`Plan failed: ${res.status} ${await res.text()}`)
  }
  return res.json() as Promise<PlanResponse>
}

export async function fetchWorkflows(): Promise<WorkflowListItem[]> {
  const res = await fetch(`${BASE}/api/workflows/list`)
  if (!res.ok) {
    throw new Error(`Fetch workflows failed: ${res.status}`)
  }
  return res.json() as Promise<WorkflowListItem[]>
}

export async function fetchWorkflow(id: string): Promise<WorkflowDetail> {
  const res = await fetch(`${BASE}/api/workflow/${id}`)
  if (!res.ok) {
    throw new Error(`Fetch workflow failed: ${res.status}`)
  }
  return res.json() as Promise<WorkflowDetail>
}

export async function fetchMetrics(): Promise<Metrics> {
  const res = await fetch(`${BASE}/api/metrics`)
  if (!res.ok) {
    throw new Error(`Fetch metrics failed: ${res.status}`)
  }
  return res.json() as Promise<Metrics>
}

// Sprint 14: Schedule API.
export async function createSchedule(name: string, goal: string, naturalSchedule: string): Promise<ScheduledJob> {
  const res = await fetch(`${BASE}/api/schedule`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name, goal, natural_schedule: naturalSchedule }),
  })
  if (!res.ok) {
    throw new Error(`Schedule create failed: ${res.status} ${await res.text()}`)
  }
  return res.json() as Promise<ScheduledJob>
}

export async function fetchSchedules(): Promise<ScheduledJob[]> {
  const res = await fetch(`${BASE}/api/schedules`)
  if (!res.ok) {
    throw new Error(`Fetch schedules failed: ${res.status}`)
  }
  return res.json() as Promise<ScheduledJob[]>
}

export async function pauseSchedule(id: string): Promise<void> {
  const res = await fetch(`${BASE}/api/schedule/${id}/pause`, { method: 'PUT' })
  if (!res.ok) throw new Error(`Pause failed: ${res.status}`)
}

export async function resumeSchedule(id: string): Promise<void> {
  const res = await fetch(`${BASE}/api/schedule/${id}/resume`, { method: 'PUT' })
  if (!res.ok) throw new Error(`Resume failed: ${res.status}`)
}

export async function deleteSchedule(id: string): Promise<void> {
  const res = await fetch(`${BASE}/api/schedule/${id}`, { method: 'DELETE' })
  if (!res.ok) throw new Error(`Delete failed: ${res.status}`)
}

export async function runScheduleNow(id: string): Promise<void> {
  const res = await fetch(`${BASE}/api/schedule/${id}/run`, { method: 'POST' })
  if (!res.ok) throw new Error(`Run now failed: ${res.status}`)
}

// ─── Deep Research API ─────────────────────────────────────────────────────

export async function startResearch(goal: string): Promise<{ session_id: string; status: string }> {
  const res = await fetch(`${BASE}/api/research`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ goal }),
  })
  if (!res.ok) {
    throw new Error(`Research start failed: ${res.status} ${await res.text()}`)
  }
  return res.json()
}

export async function fetchResearchSessions(limit = 20): Promise<ResearchSession[]> {
  const res = await fetch(`${BASE}/api/research/list?limit=${limit}`)
  if (!res.ok) {
    throw new Error(`Fetch research sessions failed: ${res.status}`)
  }
  return res.json() as Promise<ResearchSession[]>
}

export async function fetchResearchDetail(id: string): Promise<ResearchSession> {
  const res = await fetch(`${BASE}/api/research/${id}`)
  if (!res.ok) {
    throw new Error(`Fetch research detail failed: ${res.status}`)
  }
  return res.json() as Promise<ResearchSession>
}

export async function fetchResearchBudget(): Promise<ResearchBudget> {
  const res = await fetch(`${BASE}/api/research/budget`)
  if (!res.ok) {
    throw new Error(`Fetch budget failed: ${res.status}`)
  }
  return res.json() as Promise<ResearchBudget>
}

// Sprint 12: Memory-aware quick chat.
export interface ChatResponse {
  reply: string
}

export async function sendChat(message: string): Promise<ChatResponse> {
  const res = await fetch(`${BASE}/api/chat`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ message }),
  })
  if (!res.ok) {
    throw new Error(`Chat failed: ${res.status} ${await res.text()}`)
  }
  return res.json() as Promise<ChatResponse>
}
