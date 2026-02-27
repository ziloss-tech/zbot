import type { PlanResponse, WorkflowListItem, WorkflowDetail, Metrics } from './types'

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
