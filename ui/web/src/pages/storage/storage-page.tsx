import { useState, useEffect, useCallback, useMemo, useRef, lazy, Suspense } from "react";
import { Info, RefreshCw, Upload } from "lucide-react";
import { useTranslation } from "react-i18next";
import { toast } from "@/stores/use-toast-store";
import { PageHeader } from "@/components/shared/page-header";
import { Button } from "@/components/ui/button";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { buildTree, mergeSubtree, setNodeLoading, formatSize, isTextFile } from "@/lib/file-helpers";
import { FileBrowser } from "@/components/shared/file-browser";
import { useStorage, useStorageSize } from "./hooks/use-storage";
import { useHttp } from "@/hooks/use-ws";

const FileUploadDialog = lazy(() =>
  import("@/components/shared/file-upload-dialog").then((m) => ({ default: m.FileUploadDialog }))
);

export function StoragePage() {
  const { t } = useTranslation("storage");
  const http = useHttp();
  const { files, baseDir, loading, listFiles, loadSubtree, readFile, deleteFile, fetchRawBlob, createFolder, saveFile } = useStorage();
  const { totalSize, loading: sizeLoading, refreshSize } = useStorageSize();

  const [tree, setTree] = useState(buildTree(files));
  const [activePath, setActivePath] = useState<string | null>(null);
  const [fileContent, setFileContent] = useState<{ content: string; path: string; size: number } | null>(null);
  const [contentLoading, setContentLoading] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<{ path: string; isDir: boolean } | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [uploadOpen, setUploadOpen] = useState(false);

  // Feature 1: Tree state persistence
  const [expandedPaths, setExpandedPaths] = useState<Set<string>>(new Set());
  const initialExpandDone = useRef(false);

  // Feature 2: Folder creation
  const [newFolderParent, setNewFolderParent] = useState<string | null>(null);

  // Feature 3: Rename
  const [renamingPath, setRenamingPath] = useState<string | null>(null);

  // Feature 4: File editing
  const [isEditing, setIsEditing] = useState(false);
  const [editContent, setEditContent] = useState("");
  const [saving, setSaving] = useState(false);

  // Feature 5: Multi-select + batch
  const [selectedPaths, setSelectedPaths] = useState<Set<string>>(new Set());

  // Rebuild tree when files change
  useEffect(() => {
    const newTree = buildTree(files);
    setTree(newTree);
    if (!initialExpandDone.current && newTree.length > 0) {
      initialExpandDone.current = true;
      setExpandedPaths(prev => {
        const next = new Set(prev);
        for (const node of newTree) {
          if (node.isDir) next.add(node.path);
        }
        return next;
      });
    }
  }, [files]);

  // Load file list + size on initial render
  useEffect(() => { listFiles(); refreshSize(); }, [listFiles, refreshSize]);

  const handleLoadMore = useCallback(async (path: string) => {
    setTree((prev) => setNodeLoading(prev, path, true));
    try {
      const children = await loadSubtree(path);
      setTree((prev) => mergeSubtree(prev, path, children));
    } catch {
      setTree((prev) => setNodeLoading(prev, path, false));
    }
  }, [loadSubtree]);

  const fileSizeMap = useMemo(() => {
    const m = new Map<string, number>();
    for (const f of files) if (!f.isDir) m.set(f.path, f.size);
    return m;
  }, [files]);

  // Feature 1: Toggle expand handler
  const handleToggleExpand = useCallback((path: string, expanded: boolean) => {
    setExpandedPaths(prev => {
      const next = new Set(prev);
      if (expanded) { next.add(path); } else { next.delete(path); }
      return next;
    });
  }, []);

  const handleSelect = useCallback(async (path: string) => {
    setActivePath(path);
    setIsEditing(false);
    setEditContent("");
    if (selectedPaths.size > 0) setSelectedPaths(new Set());
    if (isTextFile(path)) {
      setContentLoading(true);
      try {
        const res = await readFile(path);
        setFileContent(res);
      } catch {
        setFileContent(null);
      } finally {
        setContentLoading(false);
      }
    } else {
      const size = fileSizeMap.get(path) ?? 0;
      setFileContent({ content: "", path, size });
    }
  }, [readFile, fileSizeMap, selectedPaths]);

  const handleDeleteRequest = useCallback((path: string, isDir: boolean) => {
    setDeleteTarget({ path, isDir });
  }, []);

  const handleDeleteConfirm = useCallback(async () => {
    if (!deleteTarget) return;
    setDeleting(true);
    try {
      await deleteFile(deleteTarget.path);
      if (activePath === deleteTarget.path || (deleteTarget.isDir && activePath?.startsWith(deleteTarget.path + "/"))) {
        setActivePath(null);
        setFileContent(null);
      }
      await listFiles();
    } finally {
      setDeleting(false);
      setDeleteTarget(null);
    }
  }, [deleteTarget, deleteFile, listFiles, activePath]);

  // Feature 5: Batch delete
  const handleBatchDelete = useCallback(() => {
    setDeleteTarget({ path: `__batch__${selectedPaths.size}`, isDir: false });
  }, [selectedPaths]);

  const handleBatchDeleteConfirm = useCallback(async () => {
    setDeleting(true);
    const paths = Array.from(selectedPaths);
    try {
      for (const path of paths) {
        try { await deleteFile(path); } catch { /* individual errors handled */ }
      }
      setSelectedPaths(new Set());
      setDeleteTarget(null);
      if (activePath && paths.some(p => activePath === p || activePath.startsWith(p + "/"))) {
        setActivePath(null);
        setFileContent(null);
      }
      await listFiles();
    } finally {
      setDeleting(false);
    }
  }, [selectedPaths, deleteFile, listFiles, activePath]);

  // Feature 5: Select/clear
  const handleSelectNode = useCallback((path: string, selected: boolean) => {
    setSelectedPaths(prev => {
      const next = new Set(prev);
      if (selected) { next.add(path); } else { next.delete(path); }
      return next;
    });
  }, []);

  const handleClearSelection = useCallback(() => {
    setSelectedPaths(new Set());
  }, []);

  const handleDownload = useCallback(async (path: string) => {
    try {
      const blob = await fetchRawBlob(path, true);
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = path.split("/").pop() ?? "download";
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      URL.revokeObjectURL(url);
    } catch { /* silent fail */ }
  }, [fetchRawBlob]);

  const handleFetchBlob = useCallback(async (path: string) => {
    return fetchRawBlob(path, false);
  }, [fetchRawBlob]);

  const handleRefresh = useCallback(() => {
    listFiles();
    refreshSize();
  }, [listFiles, refreshSize]);

  const activeFolder = useMemo(() => {
    if (!activePath) return "";
    const idx = activePath.lastIndexOf("/");
    return idx > 0 ? activePath.slice(0, idx) : "";
  }, [activePath]);

  const [uploadFolder, setUploadFolder] = useState("");

  const handleUploadFile = useCallback(async (file: File) => {
    const params: Record<string, string> = {};
    if (uploadFolder) params["path"] = uploadFolder;
    const fd = new FormData();
    fd.append("file", file);
    await http.upload(`/v1/storage/files?` + new URLSearchParams(params).toString(), fd);
  }, [http, uploadFolder]);

  const handleUploadClose = useCallback((v: boolean) => {
    setUploadOpen(v);
    if (!v) handleRefresh();
  }, [handleRefresh]);

  const handleMove = useCallback(async (fromPath: string, toFolder: string) => {
    const fileName = fromPath.split("/").pop() ?? fromPath;
    const newPath = toFolder ? `${toFolder}/${fileName}` : fileName;
    if (fromPath === newPath) return;
    try {
      await http.put(`/v1/storage/move?from=${encodeURIComponent(fromPath)}&to=${encodeURIComponent(newPath)}`);
      if (activePath === fromPath || activePath?.startsWith(fromPath + "/")) {
        setActivePath(null);
        setFileContent(null);
      }
      setExpandedPaths(prev => {
        const next = new Set<string>();
        for (const p of prev) {
          if (p === fromPath) next.add(newPath);
          else if (p.startsWith(fromPath + "/")) next.add(newPath + p.slice(fromPath.length));
          else next.add(p);
        }
        return next;
      });
      setSelectedPaths(new Set());
      listFiles({ silent: true });
    } catch (err) {
      const msg = err instanceof Error ? err.message : "Move failed";
      toast.error(msg);
    }
  }, [http, listFiles, activePath]);

  // Feature 2: Folder creation
  const handleCreateFolder = useCallback(async (name: string) => {
    if (!name.trim()) return;
    const parentPath = newFolderParent ?? "";
    const fullPath = parentPath ? `${parentPath}/${name.trim()}` : name.trim();
    try {
      await createFolder(fullPath);
      if (parentPath) {
        setExpandedPaths(prev => { const n = new Set(prev); n.add(parentPath); return n; });
      }
      listFiles({ silent: true });
      toast.success(t("toast.folderCreated"));
    } catch {
      toast.error(t("folderCreateFailed"));
    }
    setNewFolderParent(null);
  }, [createFolder, newFolderParent, listFiles, t]);

  // Feature 3: Rename
  const handleRename = useCallback(async (path: string, newName: string) => {
    if (!newName.trim() || !renamingPath) { setRenamingPath(null); return; }
    const currentName = path.split("/").pop() ?? path;
    if (newName.trim() === currentName) { setRenamingPath(null); return; }
    const parentPath = path.includes("/") ? path.substring(0, path.lastIndexOf("/")) : "";
    const newPath = parentPath ? `${parentPath}/${newName.trim()}` : newName.trim();
    try {
      await http.put(`/v1/storage/move?from=${encodeURIComponent(path)}&to=${encodeURIComponent(newPath)}`);
      if (activePath === path) {
        setActivePath(newPath);
        if (fileContent?.path === path) setFileContent({ ...fileContent, path: newPath });
      } else if (activePath?.startsWith(path + "/")) {
        const updatedActive = newPath + activePath.slice(path.length);
        setActivePath(updatedActive);
        if (fileContent?.path === activePath) setFileContent({ ...fileContent, path: updatedActive });
      }
      if (newPath !== path) {
        setExpandedPaths(prev => {
          const next = new Set<string>();
          for (const p of prev) {
            if (p === path) next.add(newPath);
            else if (p.startsWith(path + "/")) next.add(newPath + p.slice(path.length));
            else next.add(p);
          }
          return next;
        });
      }
      setSelectedPaths(new Set());
      listFiles({ silent: true });
      toast.success(t("toast.renamed"));
    } catch {
      toast.error(t("renameFailed"));
    }
    setRenamingPath(null);
  }, [renamingPath, http, listFiles, activePath, fileContent, t]);

  // Feature 4: Edit handlers
  const handleStartEdit = useCallback(() => {
    if (!fileContent) return;
    setEditContent(fileContent.content);
    setIsEditing(true);
  }, [fileContent]);

  const handleCancelEdit = useCallback(() => {
    setIsEditing(false);
    setEditContent("");
  }, []);

  const handleSaveEdit = useCallback(async () => {
    if (!activePath) return;
    setSaving(true);
    try {
      await saveFile(activePath, editContent);
      if (isTextFile(activePath)) {
        const res = await readFile(activePath);
        setFileContent(res);
      }
      setIsEditing(false);
      setEditContent("");
      listFiles({ silent: true });
      toast.success(t("toast.saved"));
    } catch {
      toast.error(t("saveFailed"));
    } finally {
      setSaving(false);
    }
  }, [activePath, editContent, saveFile, readFile, listFiles, t]);

  const isBatchDelete = deleteTarget?.path.startsWith("__batch__") ?? false;
  const batchCount = isBatchDelete ? Number(deleteTarget?.path.split("__")[2]) : 0;
  const deleteName = isBatchDelete ? "" : (deleteTarget?.path.split("/").pop() ?? "");

  const sizeDescription = useMemo(() => {
    if (!baseDir) return t("description");
    const sizeStr = sizeLoading ? `${formatSize(totalSize)}...` : formatSize(totalSize);
    return t("descriptionWithPath", { path: baseDir, size: sizeStr });
  }, [baseDir, totalSize, sizeLoading, t]);

  return (
    <div className="flex flex-col h-full p-4 sm:p-6">
      <PageHeader
        title={t("title")}
        description={
          <span className="inline-flex items-center gap-1">
            {sizeDescription}
            <TooltipProvider>
              <Tooltip>
                <TooltipTrigger asChild>
                  <Info className="h-3.5 w-3.5 text-muted-foreground/60 cursor-help shrink-0" />
                </TooltipTrigger>
                <TooltipContent side="bottom">
                  <p>{t("sizeCacheInfo")}</p>
                </TooltipContent>
              </Tooltip>
            </TooltipProvider>
          </span>
        }
        actions={
          <div className="flex items-center gap-2">
            <Button variant="outline" size="sm" onClick={() => { setUploadFolder(activeFolder); setUploadOpen(true); }}>
              <Upload className="h-4 w-4 mr-1.5" />
              {t("common:uploadLabel", "Upload")}
            </Button>
            <Button variant="outline" size="sm" onClick={handleRefresh} disabled={loading}>
              <RefreshCw className={`h-4 w-4 mr-1.5 ${loading ? "animate-spin" : ""}`} />
              {t("common:refresh", "Refresh")}
            </Button>
          </div>
        }
      />

      <div className="mt-4 flex-1 flex flex-col min-h-0">
        <FileBrowser
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
          showSize
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
          selectedPaths={selectedPaths}
          onSelectNode={handleSelectNode}
          onDeleteSelected={handleBatchDelete}
          onClearSelection={handleClearSelection}
        />
      </div>

      <Suspense fallback={null}>
        <FileUploadDialog
          open={uploadOpen}
          onOpenChange={handleUploadClose}
          onUpload={handleUploadFile}
          title={t("upload.title")}
          description={uploadFolder ? `${t("upload.description")} → ${uploadFolder}/` : t("upload.description")}
        />
      </Suspense>

      {/* Delete confirmation dialog */}
      <Dialog open={!!deleteTarget} onOpenChange={(open) => { if (!open) setDeleteTarget(null); }}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {isBatchDelete
                ? t("batch.deleteConfirmTitle", { count: batchCount })
                : deleteTarget?.isDir ? t("delete.folderTitle") : t("delete.fileTitle")}
            </DialogTitle>
            <DialogDescription>
              {isBatchDelete
                ? t("batch.deleteConfirmDesc", { count: batchCount }) + t("delete.undone")
                : t("delete.description", { name: deleteName }) + (deleteTarget?.isDir ? t("delete.folderWarning") : "") + t("delete.undone")}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDeleteTarget(null)} disabled={deleting}>
              {t("common:cancel", "Cancel")}
            </Button>
            <Button variant="destructive" onClick={isBatchDelete ? handleBatchDeleteConfirm : handleDeleteConfirm} disabled={deleting}>
              {deleting ? t("delete.deleting") : t("delete.confirmLabel")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
