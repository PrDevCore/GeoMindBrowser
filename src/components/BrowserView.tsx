import React, { useState, useEffect } from 'react';
import { BrowserTab, VpnLocation } from '../types';
import { useState, useEffect } from 'react';
import { 
  Globe, ShieldCheck, ShieldAlert, ArrowLeft, ArrowRight, RotateCw, Plus, X, 
  Lock, Bookmark, ExternalLink, AlertTriangle, ChevronDown
} from 'lucide-react';

interface BrowserViewProps {
  vpnEnabled: boolean;
  setVpnEnabled: (enabled: boolean) => void;
}

export const BrowserView: React.FC<BrowserViewProps> = ({ vpnEnabled, setVpnEnabled }) => {
  const [tabs, setTabs] = useState<BrowserTab[]>([]);
  const [activeTabId, setActiveTabId] = useState<string | null>(null);
  const [urlInput, setUrlInput] = useState<string>('');
  const [showBookmarks, setShowBookmarks] = useState<boolean>(false);
  const [showVpnDropdown, setShowVpnDropdown] = useState<boolean>(false);

  const activeTab = tabs.find(t => t.id === activeTabId) ?? tabs[0];
  const isHttps = activeTab.url.startsWith('https://');

  useEffect(() => {
    if (activeTab) {
      setUrlInput(activeTab.url);
    }
  }, [activeTabId, activeTab?.url]);

  const handleSelectTab = (tab: BrowserTab) => {
    setActiveTabId(tab.id);
    setUrlInput(tab.url);
  };

  const handleNewTab = () => {
    const newId = Date.now().toString();
    const defaultUrl = 'https://example.com';
    const newTab: BrowserTab = {
      id: newId,
      url: defaultUrl,
      title: 'Example.com',
      isLoading: false,
      history: [defaultUrl],
      historyIndex: 0,
    };
    setTabs([...tabs, newTab]);
    setActiveTabId(newId);
    setUrlInput(defaultUrl);
  };

  const handleCloseTab = (e: React.MouseEvent, id: string) => {
    e.stopPropagation();
    if (tabs.length === 1) return;
    const nextTabs = tabs.filter(t => t.id !== id);
    setTabs(nextTabs);
    if (activeTabId === id) {
      setActiveTabId(nextTabs[nextTabs.length - 1].id);
      setUrlInput(nextTabs[nextTabs.length - 1].url);
    }
  };

  const handleNavigate = (inputVal: string) => {
    let targetUrl = inputVal.trim();
    if (!targetUrl) return;

    // Check if it's a URL or a search query
    const isUrl = targetUrl.startsWith('http://') || 
                  targetUrl.startsWith('https://') || 
                  targetUrl.includes('.') && !targetUrl.includes(' ');

    if (!isUrl) {
      // It's a search query -> DuckDuckGo
      targetUrl = `https://html.duckduckgo.com/html/?q=${encodeURIComponent(targetUrl)}`;
    } else if (!targetUrl.startsWith('http://') && !targetUrl.startsWith('https://')) {
      targetUrl = 'https://' + targetUrl;
    }

    const domain = targetUrl.includes('search?q=') ? 'Search Result' : targetUrl.replace('https://', '').replace('http://', '').split('/')[0];
    const title = domain;

    setTabs(tabs.map(t => {
      if (t.id === activeTabId) {
        const newHistory = t.history.slice(0, t.historyIndex + 1);
        newHistory.push(targetUrl);
        return {
          ...t,
          url: targetUrl,
          title,
          isLoading: true,
          history: newHistory,
          historyIndex: newHistory.length - 1,
        };
      }
      return t;
    }));
    setUrlInput(targetUrl);
  };

  const markLoaded = (tabId: string) => {
    setTabs(prev => prev.map(t => t.id === tabId ? { ...t, isLoading: false } : t));
  };

  const handleFormSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    handleNavigate(urlInput);
  };

  return (
    <div className="flex flex-col h-[calc(100vh-65px)] bg-slate-100 dark:bg-slate-950 text-slate-900 dark:text-slate-100">
      {/* Browser Tab Bar */}
      <div className="flex items-center gap-1 px-4 pt-2 bg-slate-200/70 dark:bg-slate-900 border-b border-slate-200 dark:border-slate-800 overflow-x-auto">
        <div className="flex items-center gap-1.5 mr-2">
          <div className="w-3 h-3 rounded-full bg-red-400"></div>
          <div className="w-3 h-3 rounded-full bg-amber-400"></div>
          <div className="w-3 h-3 rounded-full bg-emerald-400"></div>
        </div>

        <div className="flex items-center gap-1 overflow-x-auto flex-1">
          {tabs.map(tab => {
            const isActive = tab.id === activeTabId;
            return (
              <div
                key={tab.id}
                onClick={() => handleSelectTab(tab)}
                className={`group relative flex items-center gap-2 px-4 py-2 rounded-t-xl text-xs font-medium cursor-pointer transition-all max-w-[220px] min-w-[140px] truncate ${
                  isActive
                    ? 'bg-white dark:bg-slate-950 text-slate-900 dark:text-white shadow-sm border-t-2 border-brand-500'
                    : 'text-slate-600 dark:text-slate-400 hover:bg-slate-300/50 dark:hover:bg-slate-800/50'
                }`}
              >
                <Globe className={`w-3.5 h-3.5 shrink-0 ${isActive ? 'text-brand-500' : 'text-slate-400'}`} />
                <span className="truncate flex-1">{tab.title}</span>
                {tabs.length > 1 && (
                  <button
                    onClick={(e) => handleCloseTab(e, tab.id)}
                    className="p-0.5 rounded-md hover:bg-slate-200 dark:hover:bg-slate-800 text-slate-400 hover:text-slate-700 dark:hover:text-slate-200 opacity-0 group-hover:opacity-100 transition-opacity"
                  >
                    <X className="w-3 h-3" />
                  </button>
                )}
              </div>
            );
          })}
        </div>

        <button
          onClick={handleNewTab}
          className="p-1.5 ml-2 rounded-lg text-slate-600 dark:text-slate-400 hover:bg-slate-300/60 dark:hover:bg-slate-800 transition-colors"
          title="Open New Tab"
        >
          <Plus className="w-4 h-4" />
        </button>
      </div>

      {/* Browser Address & Toolbar */}
      <div className="flex items-center gap-3 px-4 py-2.5 bg-white dark:bg-slate-900 border-b border-slate-200 dark:border-slate-800 shadow-sm flex-wrap md:flex-nowrap">
        <div className="flex items-center gap-1 text-slate-500 dark:text-slate-400">
          <button 
            disabled={activeTab.historyIndex <= 0}
            onClick={() => {
              if (activeTab.historyIndex > 0) {
                const newIdx = activeTab.historyIndex - 1;
                const targetUrl = activeTab.history[newIdx];
                const title = targetUrl.replace('https://', '').split('/')[0];
                setTabs(tabs.map(t => t.id === activeTabId ? { ...t, historyIndex: newIdx, url: targetUrl, title } : t));
                setUrlInput(targetUrl);
              }
            }}
            className="p-1.5 rounded-lg hover:bg-slate-100 dark:hover:bg-slate-800 disabled:opacity-30 transition-colors"
          >
            <ArrowLeft className="w-4 h-4" />
          </button>
          <button 
            disabled={activeTab.historyIndex >= activeTab.history.length - 1}
            onClick={() => {
              if (activeTab.historyIndex < activeTab.history.length - 1) {
                const newIdx = activeTab.historyIndex + 1;
                const targetUrl = activeTab.history[newIdx];
                const title = targetUrl.replace('https://', '').split('/')[0];
                setTabs(tabs.map(t => t.id === activeTabId ? { ...t, historyIndex: newIdx, url: targetUrl, title } : t));
                setUrlInput(targetUrl);
              }
            }}
            className="p-1.5 rounded-lg hover:bg-slate-100 dark:hover:bg-slate-800 disabled:opacity-30 transition-colors"
          >
            <ArrowRight className="w-4 h-4" />
          </button>
          <button 
            onClick={() => handleNavigate(activeTab.url)}
            className="p-1.5 rounded-lg hover:bg-slate-100 dark:hover:bg-slate-800 transition-colors"
            title="Reload Page"
          >
            <RotateCw className={`w-4 h-4 ${activeTab.isLoading ? 'animate-spin text-brand-500' : ''}`} />
          </button>
        </div>

        {/* Search Engine Selector */}
        <select
          value={selectedSearchEngine}

        {/* URL / Search Input Form Bound to iframe src */}
        <form onSubmit={handleFormSubmit} className="flex-1 flex items-center gap-2 px-3 py-1.5 bg-slate-100 dark:bg-slate-800/80 rounded-xl border border-slate-200 dark:border-slate-700 focus-within:border-brand-500 transition-all min-w-[200px]">
          <div className={`flex items-center gap-1.5 ${isHttps ? 'text-emerald-600 dark:text-emerald-400' : 'text-amber-600 dark:text-amber-400'}`}>
            {isHttps ? <Lock className="w-3.5 h-3.5" /> : <AlertTriangle className="w-3.5 h-3.5" />}
            <span className="text-[11px] font-mono font-medium uppercase tracking-wider">
              {isHttps ? 'HTTPS' : 'Not HTTPS'}
            </span>
          </div>
          <input
            type="text"
            value={urlInput}
            onChange={(e) => setUrlInput(e.target.value)}
            className="flex-1 bg-transparent text-xs font-mono text-slate-900 dark:text-slate-100 focus:outline-none"
            placeholder="Enter URL or search query and hit Enter..."
          />
        </form>

        {/* VPN Quick Toggle & Location Selector */}
        <div className="relative flex items-center gap-2">
          <button
            onClick={() => setVpnEnabled(!vpnEnabled)}
            className={`flex items-center gap-1.5 px-3 py-1.5 rounded-xl text-xs font-semibold shadow-sm transition-all whitespace-nowrap ${
              vpnEnabled
                ? 'bg-emerald-600 hover:bg-emerald-500 text-white'
                : 'bg-slate-200 dark:bg-slate-800 text-slate-700 dark:text-slate-300'
            }`}
            title="Toggle the VPN simulation (no traffic is routed)"
          >
            {vpnEnabled ? <ShieldCheck className="w-3.5 h-3.5" /> : <ShieldAlert className="w-3.5 h-3.5" />}
            <span>VPN sim: {vpnEnabled ? 'ON' : 'OFF'}</span>
          </button>

          <div className="relative">
            <button
              onClick={() => setShowVpnDropdown(!showVpnDropdown)}
              className="flex items-center gap-1.5 px-3 py-1.5 rounded-xl border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-800 text-xs font-medium text-slate-700 dark:text-slate-200 hover:bg-slate-50 dark:hover:bg-slate-700 transition-colors"
              title="Select simulated gateway"
            >
              <span>{activeVpnLocation.flag}</span>
              <span className="hidden lg:inline">{activeVpnLocation.city}</span>
              <ChevronDown className="w-3.5 h-3.5 text-slate-400" />
            </button>

            {showVpnDropdown && (
              <div className="absolute right-0 mt-2 w-64 bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-800 rounded-2xl shadow-2xl z-50 p-2 space-y-1 animate-fadeIn">
                <div className="px-3 py-2 text-[10px] font-bold uppercase tracking-wider text-slate-400 border-b border-slate-100 dark:border-slate-800">
                  Select gateway (simulated list)
                </div>
                {vpnLocations.map(loc => (
                  <button
                    key={loc.id}
                    onClick={() => {
                      setActiveVpnLocation(loc);
                      setVpnEnabled(true);
                      setShowVpnDropdown(false);
                    }}
                    className={`w-full flex items-center justify-between p-2.5 rounded-xl text-xs text-left transition-colors ${
                      activeVpnLocation.id === loc.id
                        ? 'bg-brand-50 dark:bg-brand-950/50 text-brand-600 dark:text-brand-400 font-bold'
                        : 'hover:bg-slate-100 dark:hover:bg-slate-800 text-slate-700 dark:text-slate-300'
                    }`}
                  >
                    <div className="flex items-center gap-2.5">
                      <span className="text-base">{loc.flag}</span>
                      <div>
                        <div>{loc.city}</div>
                        <div className="text-[10px] text-slate-400 font-mono">{loc.country}</div>
                      </div>
                    </div>
                    <span className="text-[10px] font-mono text-emerald-600 dark:text-emerald-400">{loc.ping}ms</span>
                  </button>
                ))}
              </div>
            )}
          </div>
        </div>

        {/* View Toggle & Tools */}
        <div className="flex items-center gap-2">
          <button
            onClick={() => setShowBookmarks(!showBookmarks)}
            className={`p-2 rounded-lg text-xs font-medium transition-colors flex items-center gap-1.5 ${
              showBookmarks ? 'bg-brand-50 dark:bg-brand-950 text-brand-600 dark:text-brand-400' : 'text-slate-600 dark:text-slate-300 hover:bg-slate-100 dark:hover:bg-slate-800'
            }`}
            title="Bookmarks & Presets"
          >
            <Bookmark className="w-4 h-4" />
          </button>

          <button
            onClick={() => window.open(activeTab.url, '_blank', 'noopener,noreferrer')}
            className="p-2 rounded-lg text-xs font-medium text-slate-600 dark:text-slate-300 hover:bg-slate-100 dark:hover:bg-slate-800 transition-colors"
            title="Open in a real browser tab"
          >
            <ExternalLink className="w-4 h-4" />
          </button>
        </div>
      </div>

      {/* Bookmarks Quick Drawer */}
      {showBookmarks && (
        <div className="bg-white dark:bg-slate-900 border-b border-slate-200 dark:border-slate-800 px-6 py-3 shadow-md animate-fadeIn">
          <div className="flex items-center justify-between mb-2">
            <h3 className="text-xs font-bold uppercase tracking-wider text-slate-500 dark:text-slate-400">
              Bookmarks
            </h3>
            <button onClick={() => setShowBookmarks(false)} className="text-slate-400 hover:text-slate-600 dark:hover:text-slate-200">
              <X className="w-4 h-4" />
            </button>
          </div>
          <p className="text-[10px] text-slate-500 dark:text-slate-400 mb-2">
            No bookmarks are available from the real backend yet.
          </p>
          <div className="flex items-center justify-center py-6 text-[10px] text-slate-400">
            No saved bookmarks
          </div>
              <button
                key={idx}
                onClick={() => {
                  handleNavigate(preset.url);
                  setShowBookmarks(false);
                }}
                className="flex items-center gap-2.5 p-2.5 rounded-xl border border-slate-200 dark:border-slate-800 hover:border-brand-500 bg-slate-50 dark:bg-slate-800/40 text-left transition-all group"
              >
                <span className="text-lg">{preset.icon}</span>
                <div className="truncate flex-1">
                  <div className="text-xs font-semibold text-slate-900 dark:text-white truncate group-hover:text-brand-600">
                    {preset.title}
                  </div>
                  <div className="text-[10px] text-slate-500 dark:text-slate-400 font-mono truncate">
                    {preset.category}
                  </div>
                </div>
              </button>
            ))}
          </div>
        </div>
      )}

      {/* Main Browser Viewport */}
      <div className="flex-1 flex flex-col md:flex-row overflow-hidden relative">
        <div className="flex-1 flex flex-col bg-white dark:bg-slate-900 overflow-hidden relative">
          {activeTab.isLoading ? (
            <div className="flex-1 flex flex-col items-center justify-center gap-3 p-12">
              <div className="w-10 h-10 border-4 border-brand-600 border-t-transparent rounded-full animate-spin"></div>
              <p className="text-sm font-medium text-slate-600 dark:text-slate-400">
                Loading {activeTab.url}…
              </p>
            </div>
          ) : (
            <div className="flex-1 flex flex-col w-full h-full relative">
              <iframe
                key={activeTab.url}
                src={activeTab.url}
                title={activeTab.title}
                className="w-full h-full border-0 flex-1"
                sandbox="allow-scripts allow-same-origin allow-forms allow-popups"
                onLoad={() => markLoaded(activeTab.id)}
              />
            </div>
          )}
        </div>
      </div>
    </div>
  );
};
