// Main Storage tab for Settings — composes file browser, upload dialog, delete confirm.
// Calls /v1/storage/* REST endpoints via useStorage + useStorageSize hooks.

import { useState, useEffect, useCallback, useMemo, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { useStorage, useStorageSize } from '../../../hooks/use-storage'
import { buildTree, mergeSubtree, setNodeLoading, formatSize, isTextFile } from '../../../lib/file-helpers'
import { getApiClient } from '../../../lib/api'
import { wails } from '../../../lib/wails'
import { ConfirmDialog } from '../../common/ConfirmDialog'
import { RefreshButton } from '../../common/RefreshButton'
import { StorageFileBrowser } from './StorageFileBrowser'
import { StorageUploadDialog } from './StorageUploadDialog'

export function StorageTab() {
  const { t } = useTranslation('storage')
  const { t: tc } = useTranslation('common')
  const {
    files, baseDir, loading,
    listFiles, loadSubtree, readFile, deleteFile, uploadFile, moveFile, createFolder, saveFile, fetchRawBlob,
  } = useStorage()
  const { totalSize, loading: sizeLoading, refreshSize } = useStorageSize()

  const [tree, setTree] = useState(buildTree(files))
  const [activePath, setActivePath] = useState<string | null>(null)
  const [fileContent, setFileContent] = useState<{ content: string; path: string; size: number } | null>(null)
  const [contentLoading, setContentLoading] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<{ path: string; isDir: boolean } | null>(null)
  const [deleting, setDeleting] = useState(false)
  const [uploadOpen, setUploadOpen] = useState(false)
  const [uploadFolder, setUploadFolder] = useState('')
  const [expandedPaths, setExpandedPaths] = useState<Set<string>>(new Set())
  const initialExpandDone = useRef(false)
  const [newFolderParent, setNewFolderParent] = useState<string | null>(null)
  const [renamingPath, setRenamingPath] = useState<string | null>(null)
  const [isEditing, setIsEditing] = useState(false)
  const [editContent, setEditContent] = useState('')
  const [saving, setSaving] = useState(false)

  // Rebuild tree when files change — expanded state persists via expandedPaths
  useEffect(() => {
    const newTree = buildTree(files)
    setTree(newTree)
    // Auto-expand root nodes on first load
    if (!initialExpandDone.current && newTree.length > 0) {
      initialExpandDone.current = true
      setExpandedPaths(prev => {
        const next = new Set(prev)
        for (const node of newTree) {
          if (node.isDir) next.add(node.path)
        }
        return next
      })
    }
  }, [files])

  // Load on mount
  useEffect(() => { listFiles(); refreshSize() }, [listFiles, refreshSize])

  // File size map for non-text files
  const fileSizeMap = useMemo(() => {
    const m = new Map<string, number>()
    for (const f of files) if (!f.isDir) m.set(f.path, f.size)
    return m
  }, [files])

  const handleLoadMore = useCallback(async (path: string) => {
    setTree((prev) => setNodeLoading(prev, path, true))
    try {
      const children = await loadSubtree(path)
      setTree((prev) => mergeSubtree(prev, path, children))
    } catch {
      setTree((prev) => setNodeLoading(prev, path, false))
    }
  }, [loadSubtree])

  const handleSelect = useCallback(async (path: string) => {
    setActivePath(path)
    setIsEditing(false)
    if (isTextFile(path)) {
      setContentLoading(true)
      try {
        const res = await readFile(path)
        setFileContent(res)
      } catch {
        setFileContent(null)
      } finally {
        setContentLoading(false)
      }
    } else {
      const size = fileSizeMap.get(path) ?? 0
      setFileContent({ content: '', path, size })
    }
  }, [readFile, fileSizeMap])

  const handleDeleteRequest = useCallback((path: string, isDir: boolean) => {
    setDeleteTarget({ path, isDir })
  }, [])

  const handleDeleteConfirm = useCallback(async () => {
    if (!deleteTarget) return
    setDeleting(true)
    try {
      await deleteFile(deleteTarget.path)
      if (activePath === deleteTarget.path || (deleteTarget.isDir && activePath?.startsWith(deleteTarget.path + '/'))) {
        setActivePath(null)
        setFileContent(null)
      }
      // Remove deleted path (and children) from expandedPaths
      if (deleteTarget.isDir) {
        setExpandedPaths(prev => {
          const next = new Set<string>()
          for (const p of prev) {
            if (p !== deleteTarget.path && !p.startsWith(deleteTarget.path + '/')) next.add(p)
          }
          return next
        })
      } else {
        setExpandedPaths(prev => { const n = new Set(prev); n.delete(deleteTarget.path); return n })
      }
      await listFiles()
    } finally {
      setDeleting(false)
      setDeleteTarget(null)
    }
  }, [deleteTarget, deleteFile, listFiles, activePath])

  const handleDownload = useCallback(async (path: string) => {
    try {
      const api = getApiClient()
      const fileName = path.split('/').pop() ?? 'download'
      const url = `${api.getBaseUrl()}/v1/storage/files/${encodeURIComponent(path)}?raw=true&download=true`
      await wails.downloadURL(url, fileName)
    } catch { /* silent */ }
  }, [])

  const handleFetchBlob = useCallback(async (path: string) => {
    return fetchRawBlob(path, false)
  }, [fetchRawBlob])

  const handleRefresh = useCallback(async () => {
    await Promise.all([listFiles(), refreshSize()])
  }, [listFiles, refreshSize])

  const handleMove = useCallback(async (fromPath: string, toFolder: string) => {
    try {
      await moveFile(fromPath, toFolder)
      if (activePath === fromPath || activePath?.startsWith(fromPath + '/')) {
        setActivePath(null)
        setFileContent(null)
      }
      // Update expandedPaths: replace old prefix with new
      const fileName = fromPath.split('/').pop() ?? fromPath
      const newPath = toFolder ? `${toFolder}/${fileName}` : fileName
      if (newPath !== fromPath) {
        setExpandedPaths(prev => {
          const next = new Set<string>()
          for (const p of prev) {
            if (p === fromPath) {
              next.add(newPath)
            } else if (p.startsWith(fromPath + '/')) {
              next.add(newPath + p.slice(fromPath.length))
            } else {
              next.add(p)
            }
          }
          return next
        })
      }
      listFiles({ silent: true })
    } catch { /* toast handled in hook */ }
  }, [moveFile, listFiles, activePath])

  // Toggle folder expansion — persisted across tree rebuilds
  const handleToggleExpand = useCallback((path: string, expanded: boolean) => {
    setExpandedPaths(prev => {
      const next = new Set(prev)
      if (expanded) next.add(path) else next.delete(path)
      return next
    })
  }, [])

  // Create a new folder in the storage tree
  const handleCreateFolder = useCallback(async (name: string) => {
    if (!name.trim()) return
    const parentPath = newFolderParent ?? ''
    const fullPath = parentPath ? `${parentPath}/${name.trim()}` : name.trim()
    try {
      await createFolder(fullPath)
      // Auto-expand parent to show new folder
      if (parentPath) setExpandedPaths(prev => { const n = new Set(prev); n.add(parentPath); return n })
      listFiles({ silent: true })
    } catch { /* error handled by API */ }
    setNewFolderParent(null)
  }, [createFolder, newFolderParent, listFiles])


  // Start editing the active file
  const handleStartEdit = useCallback(() => {
    if (!fileContent) return
    setEditContent(fileContent.content)
    setIsEditing(true)
  }, [fileContent])

  // Cancel editing — discard changes
  const handleCancelEdit = useCallback(() => {
    setIsEditing(false)
    setEditContent('')
  }, [])

  // Save edited content
  const handleSaveEdit = useCallback(async () => {
    if (!activePath) return
    setSaving(true)
    try {
      await saveFile(activePath, editContent)
      // Refresh file content to reflect saved state
      if (isTextFile(activePath)) {
        const res = await readFile(activePath)
        setFileContent(res)
      }
      setIsEditing(false)
      setEditContent('')
      listFiles({ silent: true })
    } catch {
      // error handled by API
    } finally {
      setSaving(false)
    }
  }, [activePath, editContent, saveFile, readFile, listFiles])

  // Rename a file or folder
  const handleRename = useCallback(async (path: string, newName: string) => {
    if (!newName.trim() || !renamingPath) { setRenamingPath(null); return }
    const currentName = path.split('/').pop() ?? path
    if (newName.trim() === currentName) { setRenamingPath(null); return }
    const parentPath = path.includes('/') ? path.substring(0, path.lastIndexOf('/')) : ''
    try {
      await moveFile(path, parentPath, newName.trim())
      // Update activePath if renamed file was active
      const newPath = parentPath ? `${parentPath}/${newName.trim()}` : newName.trim()
      if (activePath === path) {
        setActivePath(newPath)
        if (fileContent?.path === path) setFileContent({ ...fileContent, path: newPath })
      } else if (activePath?.startsWith(path + '/')) {
        const updatedActive = newPath + activePath.slice(path.length)
        setActivePath(updatedActive)
        if (fileContent?.path === activePath) setFileContent({ ...fileContent, path: updatedActive })
      }
      // Update expandedPaths: replace old prefix with new
      if (newPath !== path) {
        setExpandedPaths(prev => {
          const next = new Set<string>()
          for (const p of prev) {
            if (p === path) {
              next.add(newPath)
            } else if (p.startsWith(path + '/')) {
              next.add(newPath + p.slice(path.length))
            } else {
              next.add(p)
            }
          }
          return next
        })
      }
      listFiles({ silent: true })
    } catch { /* error handled by API */ }
    setRenamingPath(null)
  }, [renamingPath, moveFile, listFiles, activePath, fileContent])

  // Active folder for scoped uploads
  const activeFolder = useMemo(() => {
    if (!activePath) return ''
    const idx = activePath.lastIndexOf('/')
    return idx > 0 ? activePath.slice(0, idx) : ''
  }, [activePath])

  const handleUploadFile = useCallback(async (file: File) => {
    await uploadFile(file, uploadFolder || undefined)
  }, [uploadFile, uploadFolder])

  const handleUploadClose = useCallback((v: boolean) => {
    setUploadOpen(v)
    if (!v) handleRefresh()
  }, [handleRefresh])

  // Size description
  const sizeStr = sizeLoading ? `${formatSize(totalSize)}...` : formatSize(totalSize)
  const deleteName = deleteTarget?.path.split('/').pop() ?? ''

  return (
    <div className="flex flex-col h-full space-y-4">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-sm font-semibold text-text-primary">{t('title')}</h2>
          <p className="text-[11px] text-text-muted mt-0.5">
            {baseDir ? t('descriptionWithPath', { path: baseDir, size: sizeStr }) : t('description')}
          </p>
        </div>
        <div className="flex items-center gap-2">
          <button
            onClick={() => { setUploadFolder(activeFolder); setUploadOpen(true) }}
            className="flex items-center gap-1.5 px-3 py-1.5 text-xs border border-border rounded-lg text-text-secondary hover:bg-surface-tertiary transition-colors"
          >
            <svg className="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
              <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" />
              <polyline points="17 8 12 3 7 8" />
              <line x1="12" y1="3" x2="12" y2="15" />
            </svg>
            {tc('uploadLabel', 'Upload')}
          </button>
          <RefreshButton onRefresh={handleRefresh} />
        </div>
      </div>

      {/* File browser */}
      <div className="flex-1 flex flex-col min-h-0">
        <StorageFileBrowser
          tree={tree}
          filesLoading={loading}
          activePath={activePath}
          onSelect={handleSelect}
          contentLoading={contentLoading}
          fileContent={fileContent}
          onDelete={handleDeleteRequest}
          onLoadMore={handleLoadMore}
          onMove={handleMove}
          onDownload={handleDownload}
          fetchBlob={handleFetchBlob}
          expandedPaths={expandedPaths}
          onToggleExpand={handleToggleExpand}
          newFolderParent={newFolderParent}
          onNewFolder={setNewFolderParent}
          onCreateFolder={handleCreateFolder}
          renamingPath={renamingPath}
          onRename={handleRename}
          onRenamingPathChange={setRenamingPath}
          isEditing={isEditing}
          editContent={editContent}
          saving={saving}
          onStartEdit={handleStartEdit}
          onCancelEdit={handleCancelEdit}
          onSaveEdit={handleSaveEdit}
          onEditContentChange={setEditContent}
          showSize
        />
      </div>

      {/* Upload dialog */}
      <StorageUploadDialog
        open={uploadOpen}
        onOpenChange={handleUploadClose}
        onUpload={handleUploadFile}
        title={t('upload.title')}
        description={uploadFolder ? `${t('upload.description')} → ${uploadFolder}/` : t('upload.description')}
      />

      {/* Delete confirmation */}
      <ConfirmDialog
        open={!!deleteTarget}
        onOpenChange={(open) => { if (!open) setDeleteTarget(null) }}
        title={deleteTarget?.isDir ? t('delete.folderTitle') : t('delete.fileTitle')}
        description={
          t('delete.description', { name: deleteName })
          + (deleteTarget?.isDir ? t('delete.folderWarning') : '')
          + t('delete.undone')
        }
        variant="destructive"
        confirmLabel={deleting ? t('delete.deleting') : t('delete.confirmLabel')}
        onConfirm={handleDeleteConfirm}
        loading={deleting}
      />
    </div>
  )
}
