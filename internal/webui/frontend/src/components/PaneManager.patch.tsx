/**
 * PaneManager Integration Patch for Visual Crawler
 * ==================================================
 * Apply these changes to your existing PaneManager.tsx
 *
 * This file shows the EXACT additions needed — not a full replacement.
 */

// ──────────────────────────────────────────────────────────
// 1. ADD IMPORTS at the top of PaneManager.tsx
// ──────────────────────────────────────────────────────────

import React from 'react';
import { BrowserPane } from './BrowserPane';
import { CrawlLogPane } from './CrawlLogPane';
import useCrawler from '../hooks/useCrawler';

// ──────────────────────────────────────────────────────────
// 2. UPDATE PaneType union — add 'browser' and 'crawl_log'
// ──────────────────────────────────────────────────────────

// BEFORE:
// type PaneType = 'chat' | 'code' | 'research' | 'terminal' | 'file_viewer' | 'memory' | 'web_preview';

// AFTER:
type PaneType =
  | 'chat'
  | 'code'
  | 'research'
  | 'terminal'
  | 'file_viewer'
  | 'memory'
  | 'web_preview'
  | 'browser'       // ← NEW: Visual Crawler browser pane
  | 'crawl_log';    // ← NEW: Visual Crawler action log

// ──────────────────────────────────────────────────────────
// 3. ADD to PANE_TEMPLATES object
// ──────────────────────────────────────────────────────────

// Add these entries to your existing PANE_TEMPLATES:
const CRAWLER_PANE_TEMPLATES = {
  browser: {
    type: 'browser' as PaneType,
    label: 'Browser',
    icon: '🌐',
    defaultSize: 40, // percentage
  },
  crawl_log: {
    type: 'crawl_log' as PaneType,
    label: 'Crawl Log',
    icon: '📋',
    defaultSize: 20, // percentage
  },
};

// ──────────────────────────────────────────────────────────
// 4. ADD the auto-split hook inside PaneManager component
// ──────────────────────────────────────────────────────────

/**
 * Inside your PaneManager component, add the useCrawler hook
 * and an effect that auto-splits the layout when crawling starts.
 */
function useCrawlerAutoSplit(/* your existing pane state setters */) {
  const crawler = useCrawler();

  // Auto-split when a crawl session starts
  // This effect triggers when sessionId changes from null to a value
  const prevSessionId = React.useRef<string | null>(null);

  React.useEffect(() => {
    if (crawler.sessionId && !prevSessionId.current) {
      // Crawl session just started — auto-split to show browser + log
      // Call your existing pane management functions to create the layout:
      //
      // setPanes([
      //   { id: 'chat', type: 'chat', size: 40 },
      //   { id: 'browser', type: 'browser', size: 40 },
      //   { id: 'crawl_log', type: 'crawl_log', size: 20 },
      // ]);
      //
      // The exact API depends on your PaneManager implementation.
      // The key is: Chat (40%) + Browser (40%) + CrawlLog (20%)
      console.log('[PaneManager] Auto-splitting for crawl session:', crawler.sessionId);
    }
    prevSessionId.current = crawler.sessionId;
  }, [crawler.sessionId]);

  return crawler;
}

// ──────────────────────────────────────────────────────────
// 5. ADD cases in the pane rendering switch/map
// ──────────────────────────────────────────────────────────

/**
 * In your pane rendering logic (where you switch on pane.type),
 * add these cases:
 */
function renderPane(pane: { type: PaneType; id: string }, crawler: ReturnType<typeof useCrawler>) {
  switch (pane.type) {
    // ... existing cases ...

    case 'browser':
      return (
        <BrowserPane
          sessionId={crawler.sessionId}
          screenshot={crawler.screenshot}
          currentURL={crawler.currentURL}
          status={crawler.status}
          gridConfig={crawler.gridConfig ?? { rows: 15, cols: 20, cellW: 64, cellH: 64, viewportW: 1280, viewportH: 960 }}
          gridVisible={true}
          onNavigate={crawler.navigate}
          onGridClick={crawler.click}
          onToggleGrid={() => {/* toggle grid visibility state */}}
          onRefresh={() => crawler.navigate(crawler.currentURL)}
        />
      );

    case 'crawl_log':
      return (
        <CrawlLogPane entries={crawler.actionLog} />
      );

    default:
      return null;
  }
}

// ──────────────────────────────────────────────────────────
// 6. SSE EVENT DETECTION for auto-opening panes
// ──────────────────────────────────────────────────────────

/**
 * If you want to auto-open the browser pane when crawl events
 * arrive (even before the user explicitly starts a session),
 * add this to your useEventBus listener:
 */
function handleEventForAutoSplit(event: { type: string }) {
  const crawlEventTypes = [
    'crawl_screenshot',
    'crawl_action',
    'crawl_status',
    'crawl_error',
    'crawl_grid_update',
  ];

  if (crawlEventTypes.includes(event.type)) {
    // Check if BrowserPane is already open
    // If not, trigger the auto-split layout
    // This ensures the panes appear even if Cortex starts
    // a crawl autonomously (not via user clicking "start")
  }
}
