// Split layout: file tree (left, resizable) + file viewer (right).
// Breadcrumb bar with file size badge and download button.

import { useState, useRef, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { FileTreePanel } from './StorageFileTree'
import { FileContentPanel } from './StorageFileViewer'
import { formatSize, isTextFile } from '../../lib/file-helpers'
import type { TreeNode } from '../../lib/file-helpers'

interface StorageFileBrowserProps {
  tree: TreeNode[]
  filesLoading: boolean
  activePath: string | null
  onSelect: (path: string) => void
  contentLoading: boolean
  fileContent: { content: string; path: string; size: number } | null
  onDelete?: (path: string, isDir: boolean) => void
  onLoadMore?: (path: string) => void
  onMove?: (fromPath: string, toFolder: string) => void
  onDownload?: (path: string) => void
  fetchBlob?: (path: string) => Promise<Blob>
  expandedPaths: Set<string>
  onToggleExpand: (path: string, expanded: boolean) => void
  newFolderParent: string | null
  onNewFolder: (parent: string | null) => void
  onCreateFolder: (name: string) => void
  renamingPath: string | null
  onRename: (path: string, newName: string) => void
  onRenamingPathChange: (path: string | null) => void
  selectedPaths: Set<string>
  onSelectNode: (path: string, selected: boolean) => void
  onDeleteSelected?: () => void
  onClearSelection?: () => void
  isEditing: boolean
  editContent: string
  saving: boolean
  onStartEdit: () => void
  onCancelEdit: () => void
  onSaveEdit: () => void
  onEditContentChange: (content: string) => void
  showSize?: boolean
}

export function StorageFileBrowser({
  tree, filesLoading, activePath, onSelect,
  contentLoading, fileContent,
  onDelete, onLoadMore, onMove, onDownload, fetchBlob, expandedPaths, onToggleExpand, newFolderParent, onNewFolder, onCreateFolder, renamingPath, onRename, onRenamingPathChange, selectedPaths, onSelectNode, onDeleteSelected, onClearSelection, isEditing, editContent, saving, onStartEdit, onCancelEdit, onSaveEdit, onEditContentChange, showSize,
}: StorageFileBrowserProps) {
  const { t } = useTranslation('common')
  const containerRef = useRef<HTMLDivElement>(null)
  const [treeWidth, setTreeWidth] = useState(220)
  const dragging = useRef(false)

  const onMouseDown = useCallback((e: React.MouseEvent) => {
    e.preventDefault()
    dragging.current = true
    const startX = e.clientX
    const startW = treeWidth

    const onMoveEvt = (ev: MouseEvent) => {
      if (!dragging.current) return
      const container = containerRef.current
      if (!container) return
      const maxW = container.offsetWidth * 0.5
      const newW = Math.max(160, Math.min(maxW, startW + ev.clientX - startX))
      setTreeWidth(newW)
    }
    const onUp = () => {
      dragging.current = false
      document.removeEventListener('mousemove', onMoveEvt)
      document.removeEventListener('mouseup', onUp)
    }
    document.addEventListener('mousemove', onMoveEvt)
    document.addEventListener('mouseup', onUp)
  }, [treeWidth])

  return (
    <div ref={containerRef} className="flex-1 flex border border-border rounded-lg overflow-hidden min-h-0">
      {/* Tree panel */}
      <div className="overflow-y-auto overscroll-contain bg-surface-tertiary/20 py-1 shrink-0" style={{ width: treeWidth }}>
        <FileTreePanel
          tree={tree}
          filesLoading={filesLoading}
          activePath={activePath}
          onSelect={onSelect}
          onDelete={onDelete}
          onLoadMore={onLoadMore}
          onMove={onMove}
          expandedPaths={expandedPaths}
          onToggleExpand={onToggleExpand}
          newFolderParent={newFolderParent}
          onNewFolder={onNewFolder}
          onCreateFolder={onCreateFolder}
          renamingPath={renamingPath}
          onRename={onRename}
          onRenamingPathChange={onRenamingPathChange}
          selectedPaths={selectedPaths}
          onSelectNode={onSelectNode}
          onDeleteSelected={onDeleteSelected}
          onClearSelection={onClearSelection}
          showSize={showSize}
        />
      </div>

      {/* Resizable divider */}
      <div
        className="w-1 cursor-col-resize bg-border hover:bg-accent/30 active:bg-accent/50 shrink-0"
        onMouseDown={onMouseDown}
      />

      {/* Viewer panel */}
      <div className="flex-1 flex flex-col min-w-0 overflow-hidden">
        {fileContent && (
          <div className="flex items-center justify-between text-[11px] text-text-muted border-b border-border px-3 py-2 shrink-0">
            <span className="font-mono truncate">{fileContent.path}</span>
            <div className="flex items-center gap-1.5 shrink-0 ml-auto">
              <span className="text-[10px] tabular-nums">{formatSize(fileContent.size)}</span>
              {isEditing ? (
                <>
                  <button
                    onClick={onSaveEdit}
                    disabled={saving}
                    className="flex items-center gap-1 px-2 py-0.5 text-[11px] bg-accent text-white rounded hover:bg-accent/90 disabled:opacity-50 transition-colors cursor-pointer"
                    title={t('save', 'Save')}
                  >
                    {saving ? t('saving', 'Saving...') : t('save', 'Save')}
                  </button>
                  <button
                    onClick={onCancelEdit}
                    disabled={saving}
                    className="flex items-center gap-1 px-2 py-0.5 text-[11px] border border-border rounded text-text-secondary hover:bg-surface-tertiary disabled:opacity-50 transition-colors cursor-pointer"
                    title={t('cancelEdit', 'Cancel')}
                  >
                    {t('cancelEdit', 'Cancel')}
                  </button>
                </>
              ) : (
                <>
                  {isTextFile(fileContent.path) && fileContent.size <= 1048576 ? (
                      <button
                        onClick={onStartEdit}
                        className="p-0.5 text-text-muted hover:text-text-primary transition-colors cursor-pointer"
                        title={t('edit', 'Edit')}
                      >
                        <svg className="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
                          <path d="M17 3a2.85 2.83 0 1 1 4 4L7.5 20.5 2 22l1.5-5.5Z" />
                          <path d="m15 5 4 4" />
                        </svg>
                      </button>
                    ) : null}
                  {onDownload && (
                    <button
                      onClick={() => onDownload(fileContent.path)}
                      className="p-0.5 text-text-muted hover:text-text-primary transition-colors cursor-pointer"
                      title={t('download')}
                    >
                      <svg className="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
                        <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" />
                        <polyline points="7 10 12 15 17 10" />
                        <line x1="12" y1="15" x2="12" y2="3" />
                      </svg>
                    </button>
                  )}
                </>
              )}
            </div>
          </div>
        )}
        <div className="flex-1 overflow-auto overscroll-contain p-3 min-h-0">
          <FileContentPanel
            fileContent={fileContent}
            contentLoading={contentLoading}
            fetchBlob={fetchBlob}
            onDownload={onDownload}
            isEditing={isEditing}
            editContent={editContent}
            onEditContentChange={onEditContentChange}
          />
        </div>
      </div>
    </div>
  )
}
