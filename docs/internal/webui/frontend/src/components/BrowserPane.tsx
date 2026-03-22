import React, { useState, useCallback, useMemo, useRef, useEffect } from 'react';
import type { GridConfig, CrawlerStatus, ActionEntry } from '../lib/crawler-types';

interface BrowserPaneProps {
  sessionId: string | null;
  screenshot: string;
  currentURL: string;
  status: CrawlerStatus;
  gridConfig: GridConfig;
  gridVisible: boolean;
  onNavigate: (url: string) => void;
  onGridClick: (cell: string) => void;
  onToggleGrid: () => void;
  onRefresh: () => void;
}

// Convert row/col to grid label (e.g., 0,0 = "A1", 2,6 = "C7")
const getCellLabel = (row: number, col: number): string => {
  const colChar = String.fromCharCode(65 + col); // A, B, C, ...
  const rowNum = row + 1;
  return `${colChar}${rowNum}`;
};

// Status indicator color
const getStatusColor = (status: CrawlerStatus): string => {
  switch (status) {
    case 'idle':
      return '#22c55e'; // green
    case 'navigating':
    case 'acting':
    case 'waiting':
      return '#00d4ff'; // cyan
    case 'stopped':
      return '#ef4444'; // red
    default:
      return '#6b7280'; // gray
  }
};

// Status indicator with pulse animation
const StatusIndicator: React.FC<{ status: CrawlerStatus }> = ({ status }) => {
  const isPulsing = status === 'navigating' || status === 'acting' || status === 'waiting';
  const color = getStatusColor(status);

  return (
    <div
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: '8px',
        padding: '6px 12px',
        backgroundColor: 'rgba(0,212,255,0.1)',
        borderRadius: '6px',
        border: '1px solid rgba(0,212,255,0.2)',
      }}
    >
      <div
        style={{
          width: '8px',
          height: '8px',
          borderRadius: '50%',
          backgroundColor: color,
          animation: isPulsing ? 'pulse 1.5s cubic-bezier(0.4, 0, 0.6, 1) infinite' : 'none',
        }}
      />
      <span style={{ fontSize: '12px', color: '#e0e0e0', textTransform: 'capitalize' }}>
        {status}
      </span>
      <style>{`
        @keyframes pulse {
          0%, 100% { opacity: 1; }
          50% { opacity: 0.5; }
        }
      `}</style>
    </div>
  );
};

// Grid cell component
const GridCell: React.FC<{
  row: number;
  col: number;
  cellW: number;
  cellH: number;
  label: string;
  isHovered: boolean;
  onClick: () => void;
  onMouseEnter: () => void;
  onMouseLeave: () => void;
}> = ({ row, col, cellW, cellH, label, isHovered, onClick, onMouseEnter, onMouseLeave }) => {
  return (
    <div
      onClick={onClick}
      onMouseEnter={onMouseEnter}
      onMouseLeave={onMouseLeave}
      style={{
        gridColumn: col + 1,
        gridRow: row + 1,
        border: '1px solid rgba(0,212,255,0.3)',
        backgroundColor: isHovered ? 'rgba(0,212,255,0.15)' : 'transparent',
        cursor: 'pointer',
        transition: 'background-color 0.2s ease',
        position: 'relative',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
      }}
    >
      {isHovered && (
        <div
          style={{
            fontSize: '8px',
            fontFamily: 'monospace',
            color: '#ffffff',
            backgroundColor: 'rgba(0,0,0,0.7)',
            padding: '2px 4px',
            borderRadius: '3px',
            whiteSpace: 'nowrap',
            pointerEvents: 'none',
          }}
        >
          {label}
        </div>
      )}
    </div>
  );
};

// Grid overlay component
const GridOverlay: React.FC<{
  gridConfig: GridConfig;
  onCellClick: (label: string) => void;
}> = ({ gridConfig, onCellClick }) => {
  const [hoveredCell, setHoveredCell] = useState<string | null>(null);

  const cells = useMemo(() => {
    const result = [];
    for (let row = 0; row < gridConfig.rows; row++) {
      for (let col = 0; col < gridConfig.cols; col++) {
        const label = getCellLabel(row, col);
        result.push({ row, col, label });
      }
    }
    return result;
  }, [gridConfig.rows, gridConfig.cols]);

  return (
    <div
      style={{
        position: 'absolute',
        top: 0,
        left: 0,
        width: '100%',
        height: '100%',
        display: 'grid',
        gridTemplateColumns: `repeat(${gridConfig.cols}, 1fr)`,
        gridTemplateRows: `repeat(${gridConfig.rows}, 1fr)`,
        gap: 0,
        pointerEvents: 'auto',
        zIndex: 10,
      }}
    >
      {cells.map(({ row, col, label }) => (
        <GridCell
          key={`${row}-${col}`}
          row={row}
          col={col}
          cellW={gridConfig.cellW}
          cellH={gridConfig.cellH}
          label={label}
          isHovered={hoveredCell === label}
          onClick={() => onCellClick(label)}
          onMouseEnter={() => setHoveredCell(label)}
          onMouseLeave={() => setHoveredCell(null)}
        />
      ))}
    </div>
  );
};

export const BrowserPane: React.FC<BrowserPaneProps> = ({
  sessionId,
  screenshot,
  currentURL,
  status,
  gridConfig,
  gridVisible,
  onNavigate,
  onGridClick,
  onToggleGrid,
  onRefresh,
}) => {
  const [urlInput, setUrlInput] = useState<string>(currentURL);
  const screenshotRef = useRef<HTMLImageElement>(null);

  useEffect(() => {
    setUrlInput(currentURL);
  }, [currentURL]);

  const handleNavigate = useCallback(() => {
    if (urlInput.trim()) {
      onNavigate(urlInput.trim());
    }
  }, [urlInput, onNavigate]);

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLInputElement>) => {
      if (e.key === 'Enter') {
        handleNavigate();
      }
    },
    [handleNavigate]
  );

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
      {/* URL Bar */}
      <div
        style={{
          padding: '16px',
          backgroundColor: '#0a0a1a',
          borderBottom: '1px solid rgba(0,212,255,0.1)',
        }}
      >
        <input
          type="text"
          value={urlInput}
          onChange={(e) => setUrlInput(e.target.value)}
          onKeyDown={handleKeyDown}
          placeholder="Enter URL..."
          style={{
            width: '100%',
            padding: '10px 12px',
            backgroundColor: '#1a1a3e',
            color: '#e0e0e0',
            border: '1px solid rgba(0,212,255,0.2)',
            borderRadius: '6px',
            fontFamily: 'monospace',
            fontSize: '13px',
            outline: 'none',
            transition: 'border-color 0.2s ease',
          }}
          onFocus={(e) => {
            e.currentTarget.style.borderColor = 'rgba(0,212,255,0.5)';
          }}
          onBlur={(e) => {
            e.currentTarget.style.borderColor = 'rgba(0,212,255,0.2)';
          }}
        />
      </div>

      {/* Mini Toolbar */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          padding: '12px 16px',
          backgroundColor: '#0a0a1a',
          borderBottom: '1px solid rgba(0,212,255,0.1)',
          gap: '12px',
        }}
      >
        <div style={{ display: 'flex', gap: '8px' }}>
          <button
            onClick={onRefresh}
            disabled={!sessionId}
            style={{
              padding: '6px 12px',
              backgroundColor: sessionId ? '#1a1a3e' : '#0d0d2b',
              color: sessionId ? '#e0e0e0' : '#6b7280',
              border: `1px solid ${sessionId ? 'rgba(0,212,255,0.2)' : 'rgba(107,114,128,0.2)'}`,
              borderRadius: '4px',
              cursor: sessionId ? 'pointer' : 'not-allowed',
              fontSize: '12px',
              fontWeight: '500',
              transition: 'all 0.2s ease',
            }}
            onMouseEnter={(e) => {
              if (sessionId) {
                e.currentTarget.style.backgroundColor = '#2a2a4e';
                e.currentTarget.style.borderColor = 'rgba(0,212,255,0.4)';
              }
            }}
            onMouseLeave={(e) => {
              if (sessionId) {
                e.currentTarget.style.backgroundColor = '#1a1a3e';
                e.currentTarget.style.borderColor = 'rgba(0,212,255,0.2)';
              }
            }}
          >
            ↻ Refresh
          </button>

          <button
            onClick={onToggleGrid}
            disabled={!sessionId}
            style={{
              padding: '6px 12px',
              backgroundColor: gridVisible && sessionId ? '#1a3a3e' : sessionId ? '#1a1a3e' : '#0d0d2b',
              color: sessionId ? '#e0e0e0' : '#6b7280',
              border: `1px solid ${gridVisible && sessionId ? 'rgba(0,212,255,0.4)' : sessionId ? 'rgba(0,212,255,0.2)' : 'rgba(107,114,128,0.2)'}`,
              borderRadius: '4px',
              cursor: sessionId ? 'pointer' : 'not-allowed',
              fontSize: '12px',
              fontWeight: '500',
              transition: 'all 0.2s ease',
            }}
            onMouseEnter={(e) => {
              if (sessionId) {
                e.currentTarget.style.backgroundColor = '#2a2a4e';
                e.currentTarget.style.borderColor = 'rgba(0,212,255,0.4)';
              }
            }}
            onMouseLeave={(e) => {
              if (sessionId) {
                e.currentTarget.style.backgroundColor = gridVisible ? '#1a3a3e' : '#1a1a3e';
                e.currentTarget.style.borderColor = gridVisible ? 'rgba(0,212,255,0.4)' : 'rgba(0,212,255,0.2)';
              }
            }}
          >
            ⊞ Grid
          </button>
        </div>

        <StatusIndicator status={status} />
      </div>

      {/* Screenshot Viewer */}
      <div
        style={{
          flex: 1,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          backgroundColor: '#0a0a1a',
          overflow: 'auto',
          position: 'relative',
        }}
      >
        {screenshot ? (
          <div
            style={{
              position: 'relative',
              width: '100%',
              height: '100%',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
            }}
          >
            <img
              ref={screenshotRef}
              src={`data:image/png;base64,${screenshot}`}
              alt="Browser screenshot"
              style={{
                maxWidth: '100%',
                maxHeight: '100%',
                objectFit: 'contain',
                display: 'block',
              }}
            />

            {/* Grid Overlay */}
            {gridVisible && gridConfig && (
              <GridOverlay gridConfig={gridConfig} onCellClick={onGridClick} />
            )}
          </div>
        ) : (
          <div
            style={{
              textAlign: 'center',
              color: '#6b7280',
              fontSize: '14px',
            }}
          >
            <p>No screenshot yet</p>
            <p style={{ fontSize: '12px', marginTop: '8px', color: '#4b5563' }}>
              {sessionId ? 'Navigate to a URL to see content' : 'Start a crawl to begin'}
            </p>
          </div>
        )}
      </div>
    </div>
  );
};

export default BrowserPane;
