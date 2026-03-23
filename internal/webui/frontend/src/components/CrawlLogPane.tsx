import React, { useRef, useEffect, useState, useMemo } from 'react';
import type { ActionEntry } from '../lib/crawler-types';

interface CrawlLogPaneProps {
  entries: ActionEntry[];
}

// Format ISO timestamp to HH:MM:SS
const formatTime = (isoString: string): string => {
  try {
    const date = new Date(isoString);
    return date.toLocaleTimeString('en-US', {
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
      hour12: false,
    });
  } catch {
    return '00:00:00';
  }
};

// Get badge color based on action type
const getActionColor = (action: string): { bg: string; text: string } => {
  const normalized = action.toLowerCase();
  if (normalized.includes('navigate')) {
    return { bg: 'rgba(0,212,255,0.2)', text: '#00d4ff' };
  } else if (normalized.includes('click')) {
    return { bg: 'rgba(245,158,11,0.2)', text: '#f59e0b' };
  } else if (normalized.includes('type')) {
    return { bg: 'rgba(34,197,94,0.2)', text: '#22c55e' };
  } else if (normalized.includes('scroll')) {
    return { bg: 'rgba(59,130,246,0.2)', text: '#3b82f6' };
  } else if (!action || action === 'error') {
    return { bg: 'rgba(239,68,68,0.2)', text: '#ef4444' };
  }
  return { bg: 'rgba(107,114,128,0.2)', text: '#9ca3af' };
};

// Truncate long text
const truncate = (text: string | undefined, maxLen: number = 50): string => {
  if (!text) return '';
  return text.length > maxLen ? text.substring(0, maxLen) + '...' : text;
};

// Action log entry row component
const LogEntry: React.FC<{
  entry: ActionEntry;
  isExpanded: boolean;
  onToggleExpand: (id: string) => void;
}> = ({ entry, isExpanded, onToggleExpand }) => {
  const actionColor = getActionColor(entry.action);
  const timeStr = formatTime(entry.timestamp);

  return (
    <div
      style={{
        borderBottom: '1px solid rgba(0,212,255,0.1)',
      }}
    >
      {/* Compact entry */}
      <div
        onClick={() => onToggleExpand(entry.id)}
        style={{
          padding: '12px 16px',
          cursor: 'pointer',
          display: 'flex',
          alignItems: 'center',
          gap: '12px',
          backgroundColor: isExpanded ? 'rgba(0,212,255,0.05)' : 'transparent',
          transition: 'background-color 0.2s ease',
        }}
        onMouseEnter={(e) => {
          e.currentTarget.style.backgroundColor = 'rgba(0,212,255,0.08)';
        }}
        onMouseLeave={(e) => {
          e.currentTarget.style.backgroundColor = isExpanded ? 'rgba(0,212,255,0.05)' : 'transparent';
        }}
      >
        {/* Timestamp */}
        <div
          style={{
            fontFamily: 'monospace',
            fontSize: '11px',
            color: '#9ca3af',
            minWidth: '60px',
          }}
        >
          {timeStr}
        </div>

        {/* Action badge */}
        <div
          style={{
            backgroundColor: actionColor.bg,
            color: actionColor.text,
            fontSize: '11px',
            fontWeight: '600',
            padding: '4px 8px',
            borderRadius: '4px',
            minWidth: '70px',
            textAlign: 'center',
            textTransform: 'uppercase',
            border: `1px solid ${actionColor.text}30`,
          }}
        >
          {entry.action}
        </div>

        {/* Details summary */}
        <div
          style={{
            flex: 1,
            fontSize: '12px',
            color: '#d1d5db',
            display: 'flex',
            alignItems: 'center',
            gap: '8px',
            minWidth: 0,
          }}
        >
          {entry.success ? (
            <span style={{ color: '#22c55e' }}>✓</span>
          ) : (
            <span style={{ color: '#ef4444' }}>✕</span>
          )}

          <div style={{ minWidth: 0, flex: 1 }}>
            {entry.action.toLowerCase() === 'navigate' && (
              <span>{truncate(entry.url, 40)}</span>
            )}
            {entry.action.toLowerCase() === 'click' && (
              <span>
                {entry.grid_cell ? `Cell: ${entry.grid_cell}` : ''}{' '}
                {entry.element_tag ? `(${entry.element_tag})` : ''}
              </span>
            )}
            {entry.action.toLowerCase() === 'type' && (
              <span>{truncate(entry.input || entry.element_text, 40)}</span>
            )}
            {entry.error && (
              <span style={{ color: '#ef4444' }}>{truncate(entry.error, 40)}</span>
            )}
          </div>
        </div>

        {/* Duration */}
        <div
          style={{
            fontSize: '11px',
            color: '#6b7280',
            minWidth: '45px',
            textAlign: 'right',
          }}
        >
          {entry.duration_ms}ms
        </div>

        {/* Expand icon */}
        <div
          style={{
            color: '#6b7280',
            fontSize: '12px',
            transition: 'transform 0.2s ease',
            transform: isExpanded ? 'rotate(180deg)' : 'rotate(0deg)',
          }}
        >
          ▼
        </div>
      </div>

      {/* Expanded details */}
      {isExpanded && (
        <div
          style={{
            padding: '12px 16px',
            backgroundColor: 'rgba(0,212,255,0.03)',
            borderTop: '1px solid rgba(0,212,255,0.1)',
            fontSize: '11px',
            color: '#d1d5db',
          }}
        >
          <div style={{ display: 'grid', gridTemplateColumns: 'auto 1fr', gap: '8px 16px' }}>
            {entry.timestamp && (
              <>
                <div style={{ color: '#9ca3af', fontWeight: '600' }}>Timestamp:</div>
                <div style={{ fontFamily: 'monospace', color: '#00d4ff' }}>{entry.timestamp}</div>
              </>
            )}

            {entry.url && (
              <>
                <div style={{ color: '#9ca3af', fontWeight: '600' }}>URL:</div>
                <div
                  style={{
                    fontFamily: 'monospace',
                    color: '#00d4ff',
                    wordBreak: 'break-all',
                  }}
                  title={entry.url}
                >
                  {entry.url}
                </div>
              </>
            )}

            {entry.page_title && (
              <>
                <div style={{ color: '#9ca3af', fontWeight: '600' }}>Page Title:</div>
                <div>{entry.page_title}</div>
              </>
            )}

            {entry.grid_cell && (
              <>
                <div style={{ color: '#9ca3af', fontWeight: '600' }}>Grid Cell:</div>
                <div style={{ fontFamily: 'monospace', color: '#f59e0b' }}>{entry.grid_cell}</div>
              </>
            )}

            {entry.pixel_x !== undefined && entry.pixel_y !== undefined && (
              <>
                <div style={{ color: '#9ca3af', fontWeight: '600' }}>Pixels:</div>
                <div style={{ fontFamily: 'monospace', color: '#22c55e' }}>
                  ({entry.pixel_x}, {entry.pixel_y})
                </div>
              </>
            )}

            {entry.element_tag && (
              <>
                <div style={{ color: '#9ca3af', fontWeight: '600' }}>Element Tag:</div>
                <div style={{ fontFamily: 'monospace', color: '#3b82f6' }}>&lt;{entry.element_tag}&gt;</div>
              </>
            )}

            {entry.element_text && (
              <>
                <div style={{ color: '#9ca3af', fontWeight: '600' }}>Element Text:</div>
                <div
                  style={{
                    maxHeight: '60px',
                    overflow: 'auto',
                    padding: '4px 8px',
                    backgroundColor: 'rgba(0,0,0,0.3)',
                    borderRadius: '4px',
                    wordBreak: 'break-word',
                  }}
                >
                  {entry.element_text}
                </div>
              </>
            )}

            {entry.element_attrs && Object.keys(entry.element_attrs).length > 0 && (
              <>
                <div style={{ color: '#9ca3af', fontWeight: '600' }}>Attributes:</div>
                <div
                  style={{
                    fontFamily: 'monospace',
                    fontSize: '10px',
                    maxHeight: '80px',
                    overflow: 'auto',
                    padding: '4px 8px',
                    backgroundColor: 'rgba(0,0,0,0.3)',
                    borderRadius: '4px',
                  }}
                >
                  {Object.entries(entry.element_attrs).map(([key, value]) => (
                    <div key={key}>
                      <span style={{ color: '#00d4ff' }}>{key}</span>
                      {': '}
                      <span style={{ color: '#22c55e' }}>{truncate(value, 60)}</span>
                    </div>
                  ))}
                </div>
              </>
            )}

            {entry.input && (
              <>
                <div style={{ color: '#9ca3af', fontWeight: '600' }}>Input:</div>
                <div
                  style={{
                    padding: '4px 8px',
                    backgroundColor: 'rgba(0,0,0,0.3)',
                    borderRadius: '4px',
                    wordBreak: 'break-word',
                  }}
                >
                  {entry.input}
                </div>
              </>
            )}

            {entry.error && (
              <>
                <div style={{ color: '#ef4444', fontWeight: '600' }}>Error:</div>
                <div
                  style={{
                    padding: '4px 8px',
                    backgroundColor: 'rgba(239,68,68,0.15)',
                    borderRadius: '4px',
                    color: '#fca5a5',
                    wordBreak: 'break-word',
                  }}
                >
                  {entry.error}
                </div>
              </>
            )}

            {entry.screenshot_b64 && (
              <>
                <div style={{ color: '#9ca3af', fontWeight: '600' }}>Screenshot:</div>
                <img
                  src={`data:image/png;base64,${entry.screenshot_b64}`}
                  alt="Action screenshot"
                  style={{
                    maxWidth: '200px',
                    maxHeight: '150px',
                    borderRadius: '4px',
                    border: '1px solid rgba(0,212,255,0.2)',
                  }}
                />
              </>
            )}

            {entry.duration_ms && (
              <>
                <div style={{ color: '#9ca3af', fontWeight: '600' }}>Duration:</div>
                <div style={{ fontFamily: 'monospace', color: '#9ca3af' }}>{entry.duration_ms}ms</div>
              </>
            )}
          </div>
        </div>
      )}
    </div>
  );
};

export const CrawlLogPane: React.FC<CrawlLogPaneProps> = ({ entries }) => {
  const containerRef = useRef<HTMLDivElement>(null);
  const [expandedIds, setExpandedIds] = useState<Set<string>>(new Set());

  // Auto-scroll to bottom on new entries
  useEffect(() => {
    if (containerRef.current) {
      containerRef.current.scrollTop = containerRef.current.scrollHeight;
    }
  }, [entries]);

  const handleToggleExpand = (id: string) => {
    setExpandedIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return next;
    });
  };

  return (
    <div
      style={{
        display: 'flex',
        flexDirection: 'column',
        height: '100%',
        backgroundColor: '#0d0d2b',
        borderRadius: '8px',
        overflow: 'hidden',
        border: '1px solid rgba(0,212,255,0.1)',
      }}
    >
      {/* Header */}
      <div
        style={{
          padding: '12px 16px',
          backgroundColor: '#0a0a1a',
          borderBottom: '1px solid rgba(0,212,255,0.1)',
          fontSize: '13px',
          fontWeight: '600',
          color: '#e0e0e0',
          display: 'flex',
          alignItems: 'center',
          gap: '8px',
        }}
      >
        <span>Action Log</span>
        <span
          style={{
            marginLeft: 'auto',
            fontSize: '11px',
            color: '#6b7280',
            backgroundColor: 'rgba(0,212,255,0.1)',
            padding: '2px 8px',
            borderRadius: '3px',
          }}
        >
          {entries.length} actions
        </span>
      </div>

      {/* Scrollable Log Container */}
      <div
        ref={containerRef}
        style={{
          flex: 1,
          overflow: 'auto',
          backgroundColor: '#0a0a1a',
        }}
      >
        {entries.length === 0 ? (
          <div
            style={{
              display: 'flex',
              flexDirection: 'column',
              alignItems: 'center',
              justifyContent: 'center',
              height: '100%',
              color: '#6b7280',
              fontSize: '13px',
              textAlign: 'center',
              padding: '32px 16px',
            }}
          >
            <div style={{ fontSize: '16px', marginBottom: '8px' }}>No actions yet</div>
            <div style={{ fontSize: '11px', color: '#4b5563' }}>
              Start a crawl to see activity here.
            </div>
          </div>
        ) : (
          <div>
            {entries.map((entry) => (
              <LogEntry
                key={entry.id}
                entry={entry}
                isExpanded={expandedIds.has(entry.id)}
                onToggleExpand={handleToggleExpand}
              />
            ))}
          </div>
        )}
      </div>
    </div>
  );
};

export default CrawlLogPane;
