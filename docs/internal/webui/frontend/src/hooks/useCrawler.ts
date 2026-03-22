import { useState, useCallback, useEffect, useRef } from 'react';
import type {
  CrawlerStatus,
  GridConfig,
  ActionEntry,
  CrawlEvent,
  StartSessionResponse,
} from '../lib/crawler-types';

interface UseCrawlerReturn {
  sessionId: string | null;
  screenshot: string;
  currentURL: string;
  pageTitle: string;
  status: CrawlerStatus;
  gridConfig: GridConfig | null;
  actionLog: ActionEntry[];
  isConnected: boolean;
  startCrawl: (url: string) => Promise<void>;
  navigate: (url: string) => Promise<void>;
  click: (gridCell: string) => Promise<void>;
  type: (text: string) => Promise<void>;
  scroll: (direction: 'up' | 'down' | 'left' | 'right', amount: number) => Promise<void>;
  stopCrawl: () => Promise<void>;
}

const API_BASE = '/api';

export default function useCrawler(): UseCrawlerReturn {
  const [sessionId, setSessionId] = useState<string | null>(null);
  const [screenshot, setScreenshot] = useState<string>('');
  const [currentURL, setCurrentURL] = useState<string>('');
  const [pageTitle, setPageTitle] = useState<string>('');
  const [status, setStatus] = useState<CrawlerStatus>('idle');
  const [gridConfig, setGridConfig] = useState<GridConfig | null>(null);
  const [actionLog, setActionLog] = useState<ActionEntry[]>([]);
  const [isConnected, setIsConnected] = useState<boolean>(false);

  const eventSourceRef = useRef<EventSource | null>(null);

  // Cleanup SSE connection
  const disconnectSSE = useCallback(() => {
    if (eventSourceRef.current) {
      eventSourceRef.current.close();
      eventSourceRef.current = null;
      setIsConnected(false);
    }
  }, []);

  // Connect to SSE event stream
  useEffect(() => {
    if (!sessionId) return;

    disconnectSSE();

    const eventSource = new EventSource(`${API_BASE}/events/${sessionId}`);
    eventSourceRef.current = eventSource;
    setIsConnected(true);

    eventSource.onmessage = (event: MessageEvent) => {
      try {
        const data = JSON.parse(event.data) as CrawlEvent;

        // Handle different event types
        if (data.type === 'crawl_screenshot') {
          if (data.screenshot) {
            setScreenshot(data.screenshot);
          }
          if (data.url) {
            setCurrentURL(data.url);
          }
          if (data.page_title) {
            setPageTitle(data.page_title);
          }
        } else if (data.type === 'crawl_action') {
          // Append action to log
          const entry: ActionEntry = {
            id: `action-${Date.now()}-${Math.random()}`,
            timestamp: data.timestamp,
            action: data.action,
            grid_cell: data.grid_cell,
            url: data.url || currentURL,
            success: true,
            duration_ms: 0,
            page_title: data.page_title,
            element_tag: data.element_info?.tag,
            element_text: data.element_info?.text,
            element_attrs: data.element_info?.attrs,
          };
          setActionLog((prev) => [...prev, entry]);
        } else if (data.type === 'crawl_status') {
          setStatus(data.status);
        } else if (data.type === 'crawl_error') {
          // Append error to log
          const entry: ActionEntry = {
            id: `error-${Date.now()}-${Math.random()}`,
            timestamp: data.timestamp,
            action: data.action,
            url: data.url || currentURL,
            success: false,
            error: data.error,
            duration_ms: 0,
            page_title: data.page_title,
          };
          setActionLog((prev) => [...prev, entry]);
          setStatus('idle');
        } else if (data.type === 'crawl_grid_update') {
          // Grid config update if provided in event
        }
      } catch (err) {
        console.error('Error parsing SSE event:', err);
      }
    };

    eventSource.onerror = () => {
      console.error('SSE connection error');
      setIsConnected(false);
      disconnectSSE();
    };

    return () => {
      disconnectSSE();
    };
  }, [sessionId, currentURL, disconnectSSE]);

  // Start a new crawl session
  const startCrawl = useCallback(async (url: string) => {
    try {
      setStatus('navigating');
      const response = await fetch(`${API_BASE}/crawler/start`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ url }),
      });

      if (!response.ok) {
        throw new Error(`Failed to start crawl: ${response.statusText}`);
      }

      const data = (await response.json()) as StartSessionResponse;
      setSessionId(data.session_id);
      setGridConfig(data.grid);
      setCurrentURL(url);
      setActionLog([]);
      setStatus('idle');
    } catch (err) {
      console.error('Error starting crawl:', err);
      setStatus('idle');
    }
  }, []);

  // Navigate to a URL
  const navigate = useCallback(
    async (url: string) => {
      if (!sessionId) return;
      try {
        setStatus('navigating');
        const response = await fetch(`${API_BASE}/crawler/navigate`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ session_id: sessionId, url }),
        });

        if (!response.ok) {
          throw new Error(`Navigate failed: ${response.statusText}`);
        }

        const data = await response.json();
        setCurrentURL(data.url);
        setPageTitle(data.page_title);
        setStatus('idle');
      } catch (err) {
        console.error('Error navigating:', err);
        setStatus('idle');
      }
    },
    [sessionId]
  );

  // Click on a grid cell
  const click = useCallback(
    async (gridCell: string) => {
      if (!sessionId) return;
      try {
        setStatus('acting');
        const response = await fetch(`${API_BASE}/crawler/click`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ session_id: sessionId, grid_cell: gridCell }),
        });

        if (!response.ok) {
          throw new Error(`Click failed: ${response.statusText}`);
        }

        setStatus('idle');
      } catch (err) {
        console.error('Error clicking:', err);
        setStatus('idle');
      }
    },
    [sessionId]
  );

  // Type text
  const typeText = useCallback(
    async (text: string) => {
      if (!sessionId) return;
      try {
        setStatus('acting');
        const response = await fetch(`${API_BASE}/crawler/type`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ session_id: sessionId, text }),
        });

        if (!response.ok) {
          throw new Error(`Type failed: ${response.statusText}`);
        }

        setStatus('idle');
      } catch (err) {
        console.error('Error typing:', err);
        setStatus('idle');
      }
    },
    [sessionId]
  );

  // Scroll
  const scroll = useCallback(
    async (direction: 'up' | 'down' | 'left' | 'right', amount: number) => {
      if (!sessionId) return;
      try {
        setStatus('acting');
        const response = await fetch(`${API_BASE}/crawler/scroll`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ session_id: sessionId, direction, amount }),
        });

        if (!response.ok) {
          throw new Error(`Scroll failed: ${response.statusText}`);
        }

        setStatus('idle');
      } catch (err) {
        console.error('Error scrolling:', err);
        setStatus('idle');
      }
    },
    [sessionId]
  );

  // Stop the crawl
  const stopCrawl = useCallback(async () => {
    if (!sessionId) return;
    try {
      await fetch(`${API_BASE}/crawler/stop`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ session_id: sessionId }),
      });
      setStatus('stopped');
      disconnectSSE();
      setSessionId(null);
    } catch (err) {
      console.error('Error stopping crawl:', err);
    }
  }, [sessionId, disconnectSSE]);

  return {
    sessionId,
    screenshot,
    currentURL,
    pageTitle,
    status,
    gridConfig,
    actionLog,
    isConnected,
    startCrawl,
    navigate,
    click,
    type: typeText,
    scroll,
    stopCrawl,
  };
}
