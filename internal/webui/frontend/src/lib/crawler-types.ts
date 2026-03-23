// Crawler status
export type CrawlerStatus = 'idle' | 'navigating' | 'acting' | 'waiting' | 'stopped';

// Event types from backend SSE
export type CrawlEventType = 'crawl_screenshot' | 'crawl_action' | 'crawl_status' | 'crawl_error' | 'crawl_grid_update';

// Grid configuration
export interface GridConfig {
  rows: number;
  cols: number;
  cellW: number;
  cellH: number;
  viewportW: number;
  viewportH: number;
}

// Element info from crawler
export interface ElementInfo {
  tag: string;
  text: string;
  attrs: Record<string, string>;
  bounding_box?: {
    x: number;
    y: number;
    width: number;
    height: number;
  };
  grid_cells: string[];
}

// Action log entry
export interface ActionEntry {
  id: string;
  timestamp: string;
  action: string;
  grid_cell?: string;
  pixel_x?: number;
  pixel_y?: number;
  url: string;
  input?: string;
  element_tag?: string;
  element_text?: string;
  element_attrs?: Record<string, string>;
  screenshot_b64?: string;
  success: boolean;
  error?: string;
  duration_ms: number;
  page_title?: string;
}

// Crawl event from SSE
export interface CrawlEvent {
  session_id: string;
  type: CrawlEventType;
  action: string;
  grid_cell?: string;
  url?: string;
  element_info?: ElementInfo;
  screenshot?: string;
  status: CrawlerStatus;
  error?: string;
  timestamp: string;
  page_title?: string;
}

// API response types
export interface StartSessionResponse {
  session_id: string;
  grid: GridConfig;
}

export interface NavigateResponse {
  success: boolean;
  page_title: string;
  url: string;
  screenshot: string;
}

export interface ClickResponse {
  success: boolean;
  element: ElementInfo;
  screenshot: string;
}
