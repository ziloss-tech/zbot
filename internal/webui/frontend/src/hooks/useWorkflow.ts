import { useReducer, useCallback } from 'react'
import type { SSEEvent, WorkflowState, PlannedTask, WorkflowPhase, CriticResult, ToolCallEvent } from '../lib/types'

type Action =
  | { type: 'START_PLAN'; id: string; goal: string }
  | { type: 'SSE_EVENT'; event: SSEEvent }
  | { type: 'SET_PHASE'; phase: WorkflowPhase }
  | { type: 'RESET' }

const initialState: WorkflowState = {
  id: '',
  goal: '',
  phase: 'idle',
  plannerTokens: '',
  plannedTasks: [],
  tasks: [],
  // v2 single-brain state
  agentTokens: '',
  agentThinking: '',
  modelTier: 'sonnet',
  toolCalls: [],
}

function reducer(state: WorkflowState, action: Action): WorkflowState {
  switch (action.type) {
    case 'START_PLAN':
      return {
        ...initialState,
        id: action.id,
        goal: action.goal,
        phase: 'planning',
      }

    case 'SSE_EVENT': {
      const evt = action.event

      // ─── v2: Single-brain agent events ─────────────────────────────
      if (evt.source === 'agent') {
        switch (evt.type) {
          case 'thinking':
            return {
              ...state,
              phase: state.phase === 'idle' ? 'executing' : state.phase,
              agentThinking: state.agentThinking + evt.payload,
            }
          case 'token':
            return {
              ...state,
              phase: 'executing',
              agentTokens: state.agentTokens + evt.payload,
              agentThinking: '', // clear thinking once tokens flow
            }
          case 'tool_use': {
            let toolCall: ToolCallEvent | null = null
            try {
              toolCall = JSON.parse(evt.payload) as ToolCallEvent
            } catch {
              toolCall = { name: evt.payload, input: '', status: 'running' }
            }
            return {
              ...state,
              phase: 'executing',
              toolCalls: [...state.toolCalls, toolCall],
            }
          }
          case 'status': {
            // Task status update from the single brain
            const taskID = evt.task_id ?? ''
            const existingIdx = state.tasks.findIndex((t) => t.id === taskID)
            if (existingIdx >= 0) {
              const updated = [...state.tasks]
              const existing = updated[existingIdx]
              if (existing) {
                updated[existingIdx] = {
                  ...existing,
                  status: evt.payload as 'running' | 'pending' | 'done' | 'failed',
                }
              }
              return { ...state, phase: 'executing', tasks: updated }
            }
            return {
              ...state,
              phase: 'executing',
              tasks: [
                ...state.tasks,
                {
                  id: taskID,
                  step: state.tasks.length + 1,
                  name: taskID,
                  title: taskID,
                  status: evt.payload as 'running',
                  output: '',
                  error: '',
                  depends_on: [],
                  tool_calls: [],
                },
              ],
            }
          }
          case 'complete':
            return {
              ...state,
              phase: 'complete',
              agentThinking: '',
            }
          case 'error':
            return {
              ...state,
              phase: 'error',
              error: evt.payload,
            }
        }
      }

      // ─── Legacy v1: Planner events (backwards compat) ─────────────
      if (evt.source === 'planner') {
        switch (evt.type) {
          case 'token':
            return {
              ...state,
              plannerTokens: state.plannerTokens + evt.payload,
            }
          case 'complete': {
            let tasks: PlannedTask[] = []
            try {
              tasks = JSON.parse(evt.payload) as PlannedTask[]
            } catch {
              // keep empty
            }
            return { ...state, plannedTasks: tasks }
          }
          case 'handoff':
            return {
              ...state,
              phase: 'handoff',
              dbWorkflowID: evt.payload,
            }
          case 'error':
            return { ...state, phase: 'error', error: evt.payload }
        }
      }

      // ─── Legacy v1: Executor events ───────────────────────────────
      if (evt.source === 'executor') {
        switch (evt.type) {
          case 'status': {
            const taskID = evt.task_id ?? ''
            const existingIdx = state.tasks.findIndex((t) => t.id === taskID)
            if (existingIdx >= 0) {
              const updated = [...state.tasks]
              const existing = updated[existingIdx]
              if (existing) {
                updated[existingIdx] = {
                  ...existing,
                  status: evt.payload as 'running' | 'pending' | 'done' | 'failed',
                }
              }
              return { ...state, phase: 'executing', tasks: updated }
            }
            return {
              ...state,
              phase: 'executing',
              tasks: [
                ...state.tasks,
                {
                  id: taskID,
                  step: state.tasks.length + 1,
                  name: taskID,
                  status: evt.payload as 'running',
                  output: '',
                  error: '',
                  depends_on: [],
                },
              ],
            }
          }
          case 'complete': {
            const taskID = evt.task_id ?? ''
            const updated = state.tasks.map((t) =>
              t.id === taskID ? { ...t, status: 'done' as const, output: evt.payload } : t
            )
            const allDone = updated.every((t) => t.status === 'done' || t.status === 'failed')
            return {
              ...state,
              tasks: updated,
              phase: allDone ? 'complete' : 'executing',
            }
          }
          case 'error': {
            const taskID = evt.task_id ?? ''
            const updated = state.tasks.map((t) =>
              t.id === taskID ? { ...t, status: 'failed' as const, error: evt.payload } : t
            )
            return { ...state, tasks: updated }
          }
        }
      }

      // ─── Legacy v1: Critic events ─────────────────────────────────
      if (evt.source === 'critic') {
        const taskID = evt.task_id ?? ''
        switch (evt.type) {
          case 'reviewing': {
            const updated = state.tasks.map((t) =>
              t.id === taskID ? { ...t, criticStatus: 'reviewing' as const } : t
            )
            return { ...state, tasks: updated }
          }
          case 'verdict': {
            let result: CriticResult | undefined
            try {
              result = JSON.parse(evt.payload) as CriticResult
            } catch {
              // ignore bad parse
            }
            if (!result) return state

            const verdictStatus = result.verdict === 'pass'
              ? 'passed' as const
              : result.verdict === 'partial'
                ? 'partial' as const
                : 'failed' as const

            const updated = state.tasks.map((t) =>
              t.id === taskID
                ? { ...t, criticStatus: verdictStatus, criticResult: result }
                : t
            )
            return { ...state, tasks: updated }
          }
          case 'status': {
            if (evt.payload === 'retrying') {
              const updated = state.tasks.map((t) =>
                t.id === taskID
                  ? {
                      ...t,
                      status: 'running' as const,
                      criticStatus: 'retrying' as const,
                      output: '',
                      error: '',
                    }
                  : t
              )
              return { ...state, phase: 'executing', tasks: updated }
            }
            return state
          }
        }
      }

      return state
    }

    case 'SET_PHASE':
      return { ...state, phase: action.phase }

    case 'RESET':
      return initialState

    default:
      return state
  }
}

export function useWorkflow() {
  const [state, dispatch] = useReducer(reducer, initialState)

  const startPlan = useCallback((id: string, goal: string) => {
    dispatch({ type: 'START_PLAN', id, goal })
  }, [])

  const handleSSEEvent = useCallback((event: SSEEvent) => {
    dispatch({ type: 'SSE_EVENT', event })
  }, [])

  const setPhase = useCallback((phase: WorkflowPhase) => {
    dispatch({ type: 'SET_PHASE', phase })
  }, [])

  const reset = useCallback(() => {
    dispatch({ type: 'RESET' })
  }, [])

  return { state, startPlan, handleSSEEvent, setPhase, reset }
}
