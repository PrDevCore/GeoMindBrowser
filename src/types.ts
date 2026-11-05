export type TabType = 'browser' | 'workflow' | 'docs' | 'team' | 'vpn';

export interface VpnLocation {
  id: string;
  country: string;
  city: string;
  flag: string;
  ip: string;
  ping: number;
  load: string;
}

export interface BrowserTab {
  id: string;
  url: string;
  title: string;
  favicon?: string;
  isLoading: boolean;
  history: string[];
  historyIndex: number;
}

export interface TaskItem {
  id: string;
  title: string;
  description: string;
  status: 'backlog' | 'in_progress' | 'review' | 'done';
  priority: 'low' | 'medium' | 'high' | 'urgent';
  assignee: {
    name: string;
    avatar?: string;
    role: string;
  };
  dueDate: string;
  vpnRequired: boolean;
  tags: string[];
}

export interface DocItem {
  id: string;
  title: string;
  content: string;
  lastEditedBy: string;
  updatedAt: number;
  category: string;
}

export interface TeamMember {
  id: string;
  name: string;
  role: string;
  avatar?: string;
  status: 'online' | 'busy' | 'away' | 'offline';
  currentActivity: string;
  vpnLocation: string;
}

export interface ChatMessage {
  id: string;
  senderName: string;
  senderAvatar?: string;
  text: string;
  timestamp: number;
  vpnNode: string;
}
