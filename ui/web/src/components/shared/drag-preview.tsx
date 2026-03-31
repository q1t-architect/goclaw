import {
  Folder,
  FileText,
  FileCode2,
  File,
  FileImage,
  FileJson2,
} from "lucide-react";
import { extOf, CODE_EXTENSIONS, IMAGE_EXTENSIONS } from "@/lib/file-helpers";

const cls = "h-4 w-4 shrink-0";

function PreviewIcon({ name, isDir }: { name: string; isDir: boolean }) {
  if (isDir) return <Folder className={`${cls} text-yellow-600`} />;
  const ext = extOf(name);
  if (ext === "md" || ext === "mdx") return <FileText className={`${cls} text-blue-500`} />;
  if (ext === "json" || ext === "json5") return <FileJson2 className={`${cls} text-yellow-600`} />;
  if (IMAGE_EXTENSIONS.has(ext)) return <FileImage className={`${cls} text-emerald-500`} />;
  if (CODE_EXTENSIONS.has(ext)) return <FileCode2 className={`${cls} text-orange-500`} />;
  return <File className={`${cls} text-muted-foreground`} />;
}

/** Compact drag overlay shown while dragging a file/folder in the tree. */
export function DragPreview({ name, isDir, count }: { name: string; isDir: boolean; count?: number }) {
  return (
    <div className="flex items-center gap-1.5 rounded-md border bg-popover px-3 py-1.5 text-sm shadow-lg opacity-90 pointer-events-none max-w-[240px]">
      <PreviewIcon name={name} isDir={isDir} />
      <span className="truncate">{name}</span>
      {count != null && count > 1 && (
        <span className="ml-1 px-1.5 py-0.5 rounded-full bg-primary text-primary-foreground text-[10px]">{count}</span>
      )}
    </div>
  );
}
