import React, { useState } from 'react';
import { ChatMessage, VpnLocation } from '../types';
import { useStoredList } from '../lib/store';
import { formatClockTime, formatRelativeTime } from '../lib/format';
import { Avatar } from './Avatar';
import { Users, Send, Database, MessageSquare } from 'lucide-react';

// Hardcoded fallback or proxy user info since backend data isn't referenced globally
const CURRENT_USER = {
  displayName: 'Current User',
  avatarUrl: undefined, // Or a dummy string URL if needed
};

interface TeamHubProps {
  activeVpnLocation: VpnLocation | null;
}

export const TeamHub: React.FC<TeamHubProps> = ({ activeVpnLocation }) => {
  const { items: chatMessages, update: saveChat, error } = useStoredList<ChatMessage>('chat', []);
  const [inputText, setInputText] = useState<string>('');

  const handleSendMessage = (e: React.FormEvent) => {
    e.preventDefault();
    if (!inputText.trim()) return;

    // Optional safe-guard string check for the optional chaining/null vpn node structure
    const currentNodeLabel = activeVpnLocation 
      ? `${activeVpnLocation.city} (simulated)` 
      : 'No VPN Gateway Connection';

    const newMsg: ChatMessage = {
      id: Date.now().toString(),
      senderName: CURRENT_USER.displayName,
      senderAvatar: CURRENT_USER.avatarUrl,
      text: inputText.trim(),
      timestamp: Date.now(),
      vpnNode: currentNodeLabel,
    };

    saveChat([...chatMessages, newMsg]);
    setInputText('');
  };

  return (
    <div className="flex flex-col md:flex-row h-[calc(100vh-65px)] bg-slate-50 dark:bg-slate-950 text-slate-900 dark:text-slate-100 overflow-hidden">
      {/* Team Directory Sidebar */}
      <div className="w-full md:w-80 border-r border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900 flex flex-col shrink-0">
        <div className="p-4 border-b border-slate-200 dark:border-slate-800">
          <div className="flex items-center gap-2">
            <Users className="w-4 h-4 text-emerald-500" />
            <span className="text-xs font-bold uppercase tracking-wider text-slate-700 dark:text-slate-300">Team Directory</span>
          </div>
          <p className="text-[10px] text-slate-500 dark:text-slate-400 mt-1">Sample roster · presence is not live.</p>
        </div>

        <div className="flex-1 overflow-y-auto p-3 space-y-2.5">
          <p className="text-xs text-slate-500 dark:text-slate-400">No directory data available from the backend.</p>
        </div>
      </div>

      {/* Team Chat Channel */}
      <div className="flex-1 flex flex-col bg-white dark:bg-slate-900 overflow-hidden">
        <div className="px-6 py-3 border-b border-slate-200 dark:border-slate-800 bg-slate-50/50 dark:bg-slate-950/50 flex items-center justify-between gap-3 flex-wrap">
          <div className="flex items-center gap-2">
            <MessageSquare className="w-4 h-4 text-brand-500" />
            <span className="text-xs font-bold uppercase tracking-wider text-slate-700 dark:text-slate-300">#global-engineering</span>
          </div>
          <div className="flex items-center gap-1.5 text-[11px] text-slate-500 dark:text-slate-400 font-mono">
            <Database className="w-3 h-3" />
            <span>
              {error ? `Not syncing: ${error}` : 'Messages saved to this server · single-user demo, no end-to-end encryption'}
            </span>
          </div>
        </div>

        {/* Message List */}
        <div className="flex-1 p-6 overflow-y-auto space-y-4">
          {chatMessages.map(msg => (
            <div key={msg.id} className="flex items-start gap-3">
              <Avatar name={msg.senderName} src={msg.senderAvatar} className="w-8 h-8 text-[10px] mt-1" />
              <div className="flex-1 space-y-1">
                <div className="flex items-center gap-2">
                  <span className="text-xs font-bold text-slate-900 dark:text-white">{msg.senderName}</span>
                  <span className="text-[10px] text-slate-400 font-mono" title={formatRelativeTime(msg.timestamp)}>{formatClockTime(msg.timestamp)}</span>
                  <span className="px-2 py-0.5 rounded bg-slate-100 dark:bg-slate-800 text-[9px] font-mono text-slate-500">
                    {msg.vpnNode}
                  </span>
                </div>
                <div className="p-3 rounded-2xl bg-slate-100 dark:bg-slate-800/70 text-xs text-slate-800 dark:text-slate-200 max-w-2xl">
                  {msg.text}
                </div>
              </div>
            </div>
          ))}
        </div>

        {/* Message Input Form */}
        <form onSubmit={handleSendMessage} className="p-4 border-t border-slate-200 dark:border-slate-800 bg-slate-50/50 dark:bg-slate-950/50 flex items-center gap-3">
          <input
            type="text"
            value={inputText}
            onChange={(e) => setInputText(e.target.value)}
            placeholder="Message #global-engineering..."
            className="flex-1 px-4 py-2 rounded-xl border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-800 text-xs text-slate-900 dark:text-white focus:outline-none focus:border-brand-500"
          />
          <button
            type="submit"
            className="px-4 py-2 rounded-xl bg-brand-600 text-white text-xs font-semibold hover:bg-brand-500 transition-colors shadow-sm flex items-center gap-2"
          >
            <Send className="w-3.5 h-3.5" />
            <span>Send</span>
          </button>
        </form>
      </div>
    </div>
  );
};
