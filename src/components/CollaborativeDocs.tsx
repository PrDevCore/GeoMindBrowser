import React, { useState } from 'react';
import { DocItem } from '../types';
import { CURRENT_USER, SEED_DOCS } from '../data/seed';
import { useStoredList } from '../lib/store';
import { formatRelativeTime } from '../lib/format';
import { FileText, Plus, Sparkles, Save, Check, AlertCircle, Database } from 'lucide-react';

interface CollaborativeDocsProps {
  onOpenAiDocImprover: (content: string) => void;
}

export const CollaborativeDocs: React.FC<CollaborativeDocsProps> = ({
  onOpenAiDocImprover,
}) => {
  const { items: docs, update: saveDocs, flush, saveState, error } = useStoredList<DocItem>('docs', SEED_DOCS);

  const [activeDocId, setActiveDocId] = useState<string>(SEED_DOCS[0].id);

  const activeDoc = docs.find(d => d.id === activeDocId) ?? docs[0];

  const editActiveDoc = (patch: Partial<DocItem>) => {
    saveDocs(docs.map(d =>
      d.id === activeDoc.id
        ? { ...d, ...patch, lastEditedBy: CURRENT_USER.displayName, updatedAt: Date.now() }
        : d
    ));
  };

  const handleSave = async () => {
    await flush();
  };

  const handleNewDoc = () => {
    const newId = Date.now().toString();
    const newDoc: DocItem = {
      id: newId,
      title: 'Untitled Spec Document',
      content: `# New Document\n\nStart typing your team notes or project specification here...`,
      lastEditedBy: CURRENT_USER.displayName,
      updatedAt: Date.now(),
      category: 'General',
    };
    saveDocs([newDoc, ...docs]);
    setActiveDocId(newId);
  };

  if (!activeDoc) {
    return (
      <div className="flex flex-col items-center justify-center h-[calc(100vh-65px)] gap-3 text-slate-500 dark:text-slate-400 text-sm">
        <AlertCircle className="w-5 h-5" />
        <span>No documents to show.</span>
      </div>
    );
  }

  return (
    <div className="flex flex-col md:flex-row h-[calc(100vh-65px)] bg-slate-50 dark:bg-slate-950 text-slate-900 dark:text-slate-100 overflow-hidden">
      {/* Docs Sidebar List */}
      <div className="w-full md:w-80 border-r border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900 flex flex-col shrink-0">
        <div className="p-4 border-b border-slate-200 dark:border-slate-800 flex items-center justify-between">
          <div className="flex items-center gap-2">
            <FileText className="w-4 h-4 text-amber-500" />
            <span className="text-xs font-bold uppercase tracking-wider text-slate-700 dark:text-slate-300">Shared Docs</span>
          </div>
          <button
            onClick={handleNewDoc}
            className="p-1.5 rounded-lg bg-brand-50 dark:bg-brand-950/50 text-brand-600 dark:text-brand-400 hover:bg-brand-100 transition-colors"
            title="Create New Document"
          >
            <Plus className="w-4 h-4" />
          </button>
        </div>

        <div className="flex-1 overflow-y-auto p-3 space-y-2">
          {docs.map(doc => {
            const isActive = doc.id === activeDoc.id;
            return (
              <div
                key={doc.id}
                onClick={() => setActiveDocId(doc.id)}
                className={`p-3 rounded-xl border text-left cursor-pointer transition-all ${
                  isActive
                    ? 'border-brand-500 bg-brand-50/50 dark:bg-brand-950/30 shadow-sm'
                    : 'border-slate-200 dark:border-slate-800 bg-slate-50 dark:bg-slate-800/40 hover:border-slate-300 dark:hover:border-slate-700'
                }`}
              >
                <div className="text-xs font-bold text-slate-900 dark:text-white truncate mb-1">
                  {doc.title}
                </div>
                <div className="flex items-center justify-between text-[10px] text-slate-500 dark:text-slate-400 font-mono">
                  <span>{doc.category}</span>
                  <span>Edited {formatRelativeTime(doc.updatedAt)}</span>
                </div>
              </div>
            );
          })}
        </div>
      </div>

      {/* Main Editor View */}
      <div className="flex-1 flex flex-col bg-white dark:bg-slate-900 overflow-hidden">
        {/* Editor Toolbar */}
        <div className="flex items-center justify-between px-6 py-3 border-b border-slate-200 dark:border-slate-800 bg-slate-50/50 dark:bg-slate-950/50">
          <input
            type="text"
            value={activeDoc.title}
            onChange={(e) => editActiveDoc({ title: e.target.value })}
            className="text-sm font-bold bg-transparent text-slate-900 dark:text-white focus:outline-none border-b border-transparent focus:border-brand-500 max-w-md truncate"
          />

          <div className="flex items-center gap-2">
            <button
              onClick={() => onOpenAiDocImprover(activeDoc.content)}
              className="flex items-center gap-1.5 px-3.5 py-1.5 rounded-xl bg-brand-600 text-white text-xs font-semibold hover:bg-brand-500 shadow-sm transition-all"
            >
              <Sparkles className="w-3.5 h-3.5" />
              <span>AI Improve Spec</span>
            </button>

            <button
              onClick={handleSave}
              disabled={saveState === 'saving'}
              className="flex items-center gap-1.5 px-3.5 py-1.5 rounded-xl border border-slate-200 dark:border-slate-700 text-xs font-semibold text-slate-700 dark:text-slate-200 hover:bg-slate-100 dark:hover:bg-slate-800 transition-colors disabled:opacity-60"
            >
              {saveState === 'saved'
                ? <Check className="w-3.5 h-3.5 text-emerald-500" />
                : <Save className="w-3.5 h-3.5" />}
              <span>{saveState === 'saving' ? 'Saving…' : saveState === 'saved' ? 'Saved' : 'Save now'}</span>
            </button>
          </div>
        </div>

        {/* Markdown Text Area Editor */}
        <div className="flex-1 flex flex-col p-6 overflow-y-auto max-w-4xl mx-auto w-full">
          <textarea
            value={activeDoc.content}
            onChange={(e) => editActiveDoc({ content: e.target.value })}
            className="flex-1 w-full bg-transparent text-sm font-mono text-slate-900 dark:text-slate-100 focus:outline-none resize-none leading-relaxed"
            placeholder="Write specification in Markdown..."
          />
        </div>

        {/* Status Bar */}
        <div className="px-6 py-2.5 bg-slate-50 dark:bg-slate-950 border-t border-slate-200 dark:border-slate-800 flex items-center justify-between text-xs text-slate-500 dark:text-slate-400 font-mono">
          <div className="flex items-center gap-2">
            <Database className={`w-3.5 h-3.5 ${saveState === 'error' ? 'text-red-500' : 'text-emerald-500'}`} />
            {saveState === 'error'
              ? <span>Autosave failed · {error ?? 'server unreachable'} · last edited by {activeDoc.lastEditedBy} {formatRelativeTime(activeDoc.updatedAt)}</span>
              : <span>Autosaved to data/store.json · last edited by {activeDoc.lastEditedBy} {formatRelativeTime(activeDoc.updatedAt)}</span>}
          </div>
          <div>Markdown Supported</div>
        </div>
      </div>
    </div>
  );
};
