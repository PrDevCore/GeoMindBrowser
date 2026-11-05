import React, { useState } from 'react';
import { TaskItem } from '../types';
import { SEED_TASKS, SEED_TEAM, CURRENT_USER } from '../data/seed';
import { useStoredList } from '../lib/store';
import { formatDate, initialsOf } from '../lib/format';
import {
  Kanban, Plus, Search, CloudUpload, CloudOff, CheckCircle2,
  Sparkles, Trash2, X
} from 'lucide-react';

interface WorkflowBoardProps {
  onOpenAiTaskGenerator: () => void;
}

const isoInDays = (days: number) => new Date(Date.now() + days * 86_400_000).toISOString().slice(0, 10);

export const WorkflowBoard: React.FC<WorkflowBoardProps> = ({ onOpenAiTaskGenerator }) => {
  const { items: tasks, update: saveTasks, saveState, error } = useStoredList<TaskItem>('tasks', SEED_TASKS);

  const [filterPriority, setFilterPriority] = useState<string>('all');
  const [searchQuery, setSearchQuery] = useState<string>('');
  const [showNewTaskModal, setShowNewTaskModal] = useState<boolean>(false);
  const [newTaskTitle, setNewTaskTitle] = useState<string>('');
  const [newTaskDesc, setNewTaskDesc] = useState<string>('');
  const [newTaskPriority, setNewTaskPriority] = useState<TaskItem['priority']>('medium');
  const [newTaskAssignee, setNewTaskAssignee] = useState<string>(SEED_TEAM[0].name);
  const [newTaskDueDate, setNewTaskDueDate] = useState<string>(isoInDays(7));
  const [newTaskTags, setNewTaskTags] = useState<string>('');
  const [newTaskVpnRequired, setNewTaskVpnRequired] = useState<boolean>(false);

  const filteredTasks = tasks.filter(task => {
    const matchesPriority = filterPriority === 'all' || task.priority === filterPriority;
    const matchesSearch = task.title.toLowerCase().includes(searchQuery.toLowerCase()) ||
                          task.description.toLowerCase().includes(searchQuery.toLowerCase());
    return matchesPriority && matchesSearch;
  });

  const handleMoveStatus = (taskId: string, newStatus: TaskItem['status']) => {
    saveTasks(tasks.map(t => t.id === taskId ? { ...t, status: newStatus } : t));
  };

  const handleAddTask = (e: React.FormEvent) => {
    e.preventDefault();
    if (!newTaskTitle.trim()) return;

    const assignee = SEED_TEAM.find(member => member.name === newTaskAssignee);

    const newTask: TaskItem = {
      id: Date.now().toString(),
      title: newTaskTitle.trim(),
      description: newTaskDesc.trim() || 'No additional details provided.',
      status: 'backlog',
      priority: newTaskPriority,
      assignee: {
        name: assignee?.name ?? CURRENT_USER.displayName,
        avatar: assignee?.avatar,
        role: assignee?.role ?? CURRENT_USER.role,
      },
      dueDate: newTaskDueDate,
      vpnRequired: newTaskVpnRequired,
      tags: newTaskTags.split(',').map(tag => tag.trim()).filter(Boolean),
    };

    saveTasks([newTask, ...tasks]);
    setNewTaskTitle('');
    setNewTaskDesc('');
    setNewTaskTags('');
    setShowNewTaskModal(false);
  };

  const handleDeleteTask = (taskId: string) => {
    saveTasks(tasks.filter(t => t.id !== taskId));
  };

  const columns: { id: TaskItem['status']; label: string; color: string }[] = [
    { id: 'backlog', label: 'Backlog', color: 'border-slate-300 dark:border-slate-700' },
    { id: 'in_progress', label: 'In Progress', color: 'border-blue-500' },
    { id: 'review', label: 'Review', color: 'border-amber-500' },
    { id: 'done', label: 'Completed', color: 'border-emerald-500' },
  ];

  const syncLabel = saveState === 'saving' ? 'Saving…'
    : saveState === 'error' ? 'Not saved'
    : saveState === 'saved' ? 'Saved to server'
    : 'Loaded from server';

  return (
    <div className="flex flex-col h-[calc(100vh-65px)] bg-slate-50 dark:bg-slate-950 text-slate-900 dark:text-slate-100 p-6 overflow-hidden">
      {/* Workflow Header & Actions */}
      <div className="flex flex-col md:flex-row items-start md:items-center justify-between gap-4 mb-6 shrink-0">
        <div>
          <h2 className="text-xl font-bold tracking-tight text-slate-900 dark:text-white flex items-center gap-2">
            <Kanban className="w-5 h-5 text-brand-600 dark:text-brand-400" />
            <span>Remote Team Workflow & Sprint Board</span>
          </h2>
          <p className="text-xs text-slate-500 dark:text-slate-400 mt-1 flex items-center gap-1.5">
            {saveState === 'error'
              ? <CloudOff className="w-3.5 h-3.5 text-amber-500" />
              : <CloudUpload className="w-3.5 h-3.5 text-emerald-500" />}
            <span>{syncLabel}</span>
            {error && <span className="text-slate-400">· {error}</span>}
          </p>
        </div>

        <div className="flex items-center gap-3 w-full md:w-auto">
          <div className="flex items-center gap-2 px-3 py-1.5 bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-800 shadow-sm flex-1 md:flex-initial">
            <Search className="w-3.5 h-3.5 text-slate-400" />
            <input
              type="text"
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              placeholder="Search tasks..."
              className="bg-transparent text-xs text-slate-900 dark:text-white focus:outline-none w-full md:w-48"
            />
          </div>

          <select
            value={filterPriority}
            onChange={(e) => setFilterPriority(e.target.value)}
            className="bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-800 text-slate-700 dark:text-slate-300 rounded-xl px-2.5 py-1.5 text-xs font-medium focus:outline-none cursor-pointer"
            title="Filter by priority"
          >
            <option value="all">All priorities</option>
            <option value="low">Low</option>
            <option value="medium">Medium</option>
            <option value="high">High</option>
            <option value="urgent">Urgent</option>
          </select>

          <button
            onClick={onOpenAiTaskGenerator}
            className="flex items-center gap-1.5 px-3.5 py-1.5 rounded-xl bg-brand-50 dark:bg-brand-950/50 text-brand-600 dark:text-brand-400 border border-brand-200 dark:border-brand-800 text-xs font-semibold hover:bg-brand-100 dark:hover:bg-brand-900 transition-all whitespace-nowrap"
          >
            <Sparkles className="w-3.5 h-3.5" />
            <span>AI Task Planner</span>
          </button>

          <button
            onClick={() => setShowNewTaskModal(true)}
            className="flex items-center gap-1.5 px-4 py-1.5 rounded-xl bg-brand-600 text-white text-xs font-semibold hover:bg-brand-500 transition-all shadow-sm whitespace-nowrap"
          >
            <Plus className="w-4 h-4" />
            <span>New Task</span>
          </button>
        </div>
      </div>

      {/* Kanban Columns Grid */}
      <div className="flex-1 grid grid-cols-1 md:grid-cols-4 gap-4 overflow-x-auto pb-4">
        {columns.map(col => {
          const colTasks = filteredTasks.filter(t => t.status === col.id);
          return (
            <div
              key={col.id}
              className="flex flex-col rounded-2xl bg-slate-100 dark:bg-slate-900/60 border border-slate-200/80 dark:border-slate-800 overflow-hidden"
            >
              {/* Column Header */}
              <div className={`px-4 py-3 bg-white dark:bg-slate-900 border-b-2 ${col.color} flex items-center justify-between`}>
                <span className="text-xs font-bold text-slate-800 dark:text-slate-200 uppercase tracking-wider">
                  {col.label}
                </span>
                <span className="px-2 py-0.5 rounded-full bg-slate-100 dark:bg-slate-800 text-slate-600 dark:text-slate-300 text-xs font-mono font-medium">
                  {colTasks.length}
                </span>
              </div>

              {/* Task Cards Container */}
              <div className="flex-1 p-3 overflow-y-auto space-y-3">
                {colTasks.length === 0 ? (
                  <div className="p-6 text-center text-slate-400 text-xs">
                    No tasks in {col.label}
                  </div>
                ) : (
                  colTasks.map(task => (
                    <div
                      key={task.id}
                      className="p-4 rounded-xl bg-white dark:bg-slate-950 border border-slate-200 dark:border-slate-800/80 shadow-sm space-y-3 hover:border-brand-500/50 transition-all group"
                    >
                      <div className="flex items-start justify-between gap-2">
                        <span className={`px-2 py-0.5 rounded text-[10px] font-bold uppercase tracking-wider ${
                          task.priority === 'urgent' ? 'bg-red-500/10 text-red-600 dark:text-red-400' :
                          task.priority === 'high' ? 'bg-amber-500/10 text-amber-600 dark:text-amber-400' :
                          'bg-blue-500/10 text-blue-600 dark:text-blue-400'
                        }`}>
                          {task.priority}
                        </span>
                        <button
                          onClick={() => handleDeleteTask(task.id)}
                          className="text-slate-400 hover:text-red-500 opacity-0 group-hover:opacity-100 transition-opacity"
                          title="Delete task"
                        >
                          <Trash2 className="w-3.5 h-3.5" />
                        </button>
                      </div>

                      <h4 className="text-xs font-bold text-slate-900 dark:text-white leading-snug">
                        {task.title}
                      </h4>

                      <p className="text-[11px] text-slate-600 dark:text-slate-400 line-clamp-2">
                        {task.description}
                      </p>

                      {task.tags.length > 0 && (
                        <div className="flex flex-wrap gap-1">
                          {task.tags.map((tag, i) => (
                            <span key={i} className="px-2 py-0.5 bg-slate-100 dark:bg-slate-800 text-slate-600 dark:text-slate-400 rounded text-[10px] font-medium">
                              {tag}
                            </span>
                          ))}
                        </div>
                      )}

                      <div className="flex items-center justify-between text-[11px] text-slate-500">
                        <div className="flex items-center gap-1.5">
                          <span className="w-5 h-5 rounded-full bg-brand-500/10 text-brand-600 dark:text-brand-400 flex items-center justify-center font-bold text-[9px]">
                            {initialsOf(task.assignee.name)}
                          </span>
                          <span className="truncate max-w-[90px]">{task.assignee.name}</span>
                        </div>
                        <span className="flex items-center gap-1 font-mono text-[10px]">
                          <CheckCircle2 className="w-3 h-3" />
                          {formatDate(task.dueDate)}
                        </span>
                      </div>

                      <div className="pt-2 border-t border-slate-100 dark:border-slate-800 flex items-center justify-between gap-2 text-[11px]">
                        <label className="flex items-center gap-1.5 text-slate-500 cursor-pointer">
                          <input
                            type="checkbox"
                            checked={task.vpnRequired}
                            onChange={() => saveTasks(tasks.map(t => t.id === task.id ? { ...t, vpnRequired: !t.vpnRequired } : t))}
                            className="accent-brand-600"
                          />
                          <span>Needs VPN review</span>
                        </label>

                        <select
                          value={task.status}
                          onChange={(e) => handleMoveStatus(task.id, e.target.value as TaskItem['status'])}
                          className="bg-slate-100 dark:bg-slate-800 text-slate-700 dark:text-slate-300 rounded text-[10px] font-medium px-1.5 py-1 focus:outline-none cursor-pointer"
                        >
                          <option value="backlog">Backlog</option>
                          <option value="in_progress">In Progress</option>
                          <option value="review">Review</option>
                          <option value="done">Done</option>
                        </select>
                      </div>
                    </div>
                  ))
                )}
              </div>
            </div>
          );
        })}
      </div>

      {/* New Task Modal */}
      {showNewTaskModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm p-4 animate-fadeIn">
          <div className="bg-white dark:bg-slate-900 rounded-2xl border border-slate-200 dark:border-slate-800 max-w-lg w-full p-6 shadow-2xl space-y-4">
            <div className="flex items-center justify-between pb-3 border-b border-slate-200 dark:border-slate-800">
              <h3 className="text-base font-bold text-slate-900 dark:text-white">Create New Workflow Task</h3>
              <button onClick={() => setShowNewTaskModal(false)} className="text-slate-400 hover:text-slate-600 dark:hover:text-slate-200">
                <X className="w-5 h-5" />
              </button>
            </div>

            <form onSubmit={handleAddTask} className="space-y-4">
              <div>
                <label className="block text-xs font-medium text-slate-700 dark:text-slate-300 mb-1">Task Title</label>
                <input
                  type="text"
                  required
                  value={newTaskTitle}
                  onChange={(e) => setNewTaskTitle(e.target.value)}
                  placeholder="e.g. Rotate encryption keys for the EU cluster"
                  className="w-full px-3 py-2 rounded-xl border border-slate-200 dark:border-slate-700 bg-slate-50 dark:bg-slate-800 text-xs text-slate-900 dark:text-white focus:outline-none focus:border-brand-500"
                />
              </div>

              <div>
                <label className="block text-xs font-medium text-slate-700 dark:text-slate-300 mb-1">Description & Specs</label>
                <textarea
                  rows={3}
                  value={newTaskDesc}
                  onChange={(e) => setNewTaskDesc(e.target.value)}
                  placeholder="Provide technical requirements and constraints..."
                  className="w-full px-3 py-2 rounded-xl border border-slate-200 dark:border-slate-700 bg-slate-50 dark:bg-slate-800 text-xs text-slate-900 dark:text-white focus:outline-none focus:border-brand-500"
                />
              </div>

              <div className="grid grid-cols-2 gap-4">
                <div>
                  <label className="block text-xs font-medium text-slate-700 dark:text-slate-300 mb-1">Priority Level</label>
                  <select
                    value={newTaskPriority}
                    onChange={(e) => setNewTaskPriority(e.target.value as TaskItem['priority'])}
                    className="w-full px-3 py-2 rounded-xl border border-slate-200 dark:border-slate-700 bg-slate-50 dark:bg-slate-800 text-xs text-slate-900 dark:text-white focus:outline-none"
                  >
                    <option value="low">Low Priority</option>
                    <option value="medium">Medium Priority</option>
                    <option value="high">High Priority</option>
                    <option value="urgent">Urgent</option>
                  </select>
                </div>

                <div>
                  <label className="block text-xs font-medium text-slate-700 dark:text-slate-300 mb-1">Assignee</label>
                  <select
                    value={newTaskAssignee}
                    onChange={(e) => setNewTaskAssignee(e.target.value)}
                    className="w-full px-3 py-2 rounded-xl border border-slate-200 dark:border-slate-700 bg-slate-50 dark:bg-slate-800 text-xs text-slate-900 dark:text-white focus:outline-none"
                  >
                    {SEED_TEAM.map(member => (
                      <option key={member.id} value={member.name}>{member.name} ({member.role})</option>
                    ))}
                  </select>
                </div>

                <div>
                  <label className="block text-xs font-medium text-slate-700 dark:text-slate-300 mb-1">Due Date</label>
                  <input
                    type="date"
                    value={newTaskDueDate}
                    onChange={(e) => setNewTaskDueDate(e.target.value)}
                    className="w-full px-3 py-2 rounded-xl border border-slate-200 dark:border-slate-700 bg-slate-50 dark:bg-slate-800 text-xs text-slate-900 dark:text-white focus:outline-none"
                  />
                </div>

                <div>
                  <label className="block text-xs font-medium text-slate-700 dark:text-slate-300 mb-1">Tags</label>
                  <input
                    type="text"
                    value={newTaskTags}
                    onChange={(e) => setNewTaskTags(e.target.value)}
                    placeholder="Sprint 4, Remote"
                    className="w-full px-3 py-2 rounded-xl border border-slate-200 dark:border-slate-700 bg-slate-50 dark:bg-slate-800 text-xs text-slate-900 dark:text-white focus:outline-none focus:border-brand-500"
                  />
                </div>
              </div>

              <label className="flex items-center gap-2 text-xs text-slate-700 dark:text-slate-300 cursor-pointer">
                <input
                  type="checkbox"
                  checked={newTaskVpnRequired}
                  onChange={(e) => setNewTaskVpnRequired(e.target.checked)}
                  className="accent-brand-600"
                />
                <span>Needs VPN review before completion</span>
              </label>

              <div className="pt-4 flex justify-end gap-3 border-t border-slate-200 dark:border-slate-800">
                <button
                  type="button"
                  onClick={() => setShowNewTaskModal(false)}
                  className="px-4 py-2 rounded-xl border border-slate-200 dark:border-slate-700 text-xs font-semibold text-slate-600 dark:text-slate-300 hover:bg-slate-100 dark:hover:bg-slate-800 transition-colors"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  className="px-4 py-2 rounded-xl bg-brand-600 text-white text-xs font-semibold hover:bg-brand-500 transition-colors shadow-sm"
                >
                  Create Task
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  );
};
