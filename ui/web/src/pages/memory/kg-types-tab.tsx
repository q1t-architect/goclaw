import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Plus, Pencil, Trash2, Shield } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
import { useKgEntityTypes, useKgRelationTypes } from "./hooks/use-kg-types";
import { KGTypeFormDialog } from "./kg-type-form-dialog";
import type { KGEntityType, KGRelationType } from "@/types/knowledge-graph";

interface KGTypesTabProps {
  agentId: string;
}

export function KGTypesTab({ agentId }: KGTypesTabProps) {
  const { t } = useTranslation("memory");
  const eTypes = useKgEntityTypes(agentId);
  const rTypes = useKgRelationTypes(agentId);

  const [formOpen, setFormOpen] = useState(false);
  const [editTarget, setEditTarget] = useState<KGEntityType | KGRelationType | undefined>();
  const [formKind, setFormKind] = useState<"entity" | "relation">("entity");
  const [deleteTarget, setDeleteTarget] = useState<{ id: string; kind: "entity" | "relation"; name: string } | null>(null);
  const [deleteLoading, setDeleteLoading] = useState(false);

  const openCreate = (kind: "entity" | "relation") => {
    setEditTarget(undefined);
    setFormKind(kind);
    setFormOpen(true);
  };

  const openEdit = (item: KGEntityType | KGRelationType, kind: "entity" | "relation") => {
    setEditTarget(item);
    setFormKind(kind);
    setFormOpen(true);
  };

  const handleSave = async (data: Record<string, unknown>) => {
    if (data.id) {
      if (formKind === "entity") await eTypes.updateType(data as any);
      else await rTypes.updateType(data as any);
    } else {
      if (formKind === "entity") await eTypes.createType(data);
      else await rTypes.createType(data);
    }
  };

  const handleDelete = async () => {
    if (!deleteTarget) return;
    setDeleteLoading(true);
    try {
      if (deleteTarget.kind === "entity") await eTypes.deleteType(deleteTarget.id);
      else await rTypes.deleteType(deleteTarget.id);
      setDeleteTarget(null);
    } finally {
      setDeleteLoading(false);
    }
  };

  return (
    <div className="space-y-6">
      {/* Entity Types Section */}
      <div>
        <div className="flex items-center justify-between mb-2">
          <h3 className="text-sm font-semibold">{t("kg.types.entityTypes", "Entity Types")}</h3>
          <Button size="sm" variant="outline" className="h-7 text-xs" onClick={() => openCreate("entity")}>
            <Plus className="h-3 w-3 mr-1" />{t("kg.types.addType", "Add Type")}
          </Button>
        </div>
        <div className="rounded-md border overflow-hidden">
          <table className="w-full text-sm">
            <thead className="bg-muted/50">
              <tr>
                <th className="text-left px-3 py-2 text-xs font-medium text-muted-foreground">{t("kg.types.name", "Name")}</th>
                <th className="text-left px-3 py-2 text-xs font-medium text-muted-foreground">{t("kg.types.displayName", "Display Name")}</th>
                <th className="text-left px-3 py-2 text-xs font-medium text-muted-foreground">{t("kg.types.color", "Color")}</th>
                <th className="text-left px-3 py-2 text-xs font-medium text-muted-foreground">{t("kg.types.status", "Status")}</th>
                <th className="text-right px-3 py-2 text-xs font-medium text-muted-foreground">{t("kg.types.actions", "Actions")}</th>
              </tr>
            </thead>
            <tbody className="divide-y">
              {eTypes.entityTypes.map((et) => (
                <tr key={et.id} className="hover:bg-muted/30">
                  <td className="px-3 py-2 font-mono text-xs">{et.name}</td>
                  <td className="px-3 py-2">{et.display_name}</td>
                  <td className="px-3 py-2">
                    <span className="inline-block w-4 h-4 rounded-full border" style={{ backgroundColor: et.color }} />
                  </td>
                  <td className="px-3 py-2">
                    {et.is_system && <Badge variant="secondary" className="text-[10px] h-5"><Shield className="h-3 w-3 mr-0.5" />system</Badge>}
                  </td>
                  <td className="px-3 py-2 text-right space-x-1">
                    <Button variant="ghost" size="icon" className="h-6 w-6" onClick={() => openEdit(et, "entity")}><Pencil className="h-3 w-3" /></Button>
                    <Button variant="ghost" size="icon" className="h-6 w-6" disabled={et.is_system} onClick={() => setDeleteTarget({ id: et.id, kind: "entity", name: et.name })}><Trash2 className="h-3 w-3" /></Button>
                  </td>
                </tr>
              ))}
              {eTypes.entityTypes.length === 0 && (
                <tr><td colSpan={5} className="px-3 py-4 text-center text-muted-foreground text-xs">{t("kg.types.noEntityTypes", "No entity types configured")}</td></tr>
              )}
            </tbody>
          </table>
        </div>
      </div>

      {/* Relation Types Section */}
      <div>
        <div className="flex items-center justify-between mb-2">
          <h3 className="text-sm font-semibold">{t("kg.types.relationTypes", "Relation Types")}</h3>
          <Button size="sm" variant="outline" className="h-7 text-xs" onClick={() => openCreate("relation")}>
            <Plus className="h-3 w-3 mr-1" />{t("kg.types.addType", "Add Type")}
          </Button>
        </div>
        <div className="rounded-md border overflow-hidden">
          <table className="w-full text-sm">
            <thead className="bg-muted/50">
              <tr>
                <th className="text-left px-3 py-2 text-xs font-medium text-muted-foreground">{t("kg.types.name", "Name")}</th>
                <th className="text-left px-3 py-2 text-xs font-medium text-muted-foreground">{t("kg.types.displayName", "Display Name")}</th>
                <th className="text-left px-3 py-2 text-xs font-medium text-muted-foreground">{t("kg.types.color", "Color")}</th>
                <th className="text-left px-3 py-2 text-xs font-medium text-muted-foreground">{t("kg.types.directed", "Directed")}</th>
                <th className="text-left px-3 py-2 text-xs font-medium text-muted-foreground">{t("kg.types.status", "Status")}</th>
                <th className="text-right px-3 py-2 text-xs font-medium text-muted-foreground">{t("kg.types.actions", "Actions")}</th>
              </tr>
            </thead>
            <tbody className="divide-y">
              {rTypes.relationTypes.map((rt) => (
                <tr key={rt.id} className="hover:bg-muted/30">
                  <td className="px-3 py-2 font-mono text-xs">{rt.name}</td>
                  <td className="px-3 py-2">{rt.display_name}</td>
                  <td className="px-3 py-2">
                    <span className="inline-block w-4 h-4 rounded-full border" style={{ backgroundColor: rt.color }} />
                  </td>
                  <td className="px-3 py-2 text-xs">{rt.directed ? "→" : "↔"}</td>
                  <td className="px-3 py-2">
                    {rt.is_system && <Badge variant="secondary" className="text-[10px] h-5"><Shield className="h-3 w-3 mr-0.5" />system</Badge>}
                  </td>
                  <td className="px-3 py-2 text-right space-x-1">
                    <Button variant="ghost" size="icon" className="h-6 w-6" onClick={() => openEdit(rt, "relation")}><Pencil className="h-3 w-3" /></Button>
                    <Button variant="ghost" size="icon" className="h-6 w-6" disabled={rt.is_system} onClick={() => setDeleteTarget({ id: rt.id, kind: "relation", name: rt.name })}><Trash2 className="h-3 w-3" /></Button>
                  </td>
                </tr>
              ))}
              {rTypes.relationTypes.length === 0 && (
                <tr><td colSpan={6} className="px-3 py-4 text-center text-muted-foreground text-xs">{t("kg.types.noRelationTypes", "No relation types configured")}</td></tr>
              )}
            </tbody>
          </table>
        </div>
      </div>

      {/* Form Dialog */}
      <KGTypeFormDialog
        open={formOpen}
        onClose={() => setFormOpen(false)}
        onSave={handleSave}
        existing={editTarget}
        kind={formKind}
      />

      {/* Delete Confirm */}
      <ConfirmDialog
        open={!!deleteTarget}
        onOpenChange={(v) => !v && setDeleteTarget(null)}
        title={t("kg.types.deleteConfirm", "Delete type?")}
        description={t("kg.types.deleteConfirmDesc", `Are you sure you want to delete "${deleteTarget?.name ?? ""}"?`)}
        confirmLabel={t("common:delete", "Delete")}
        onConfirm={handleDelete}
        loading={deleteLoading}
        variant="destructive"
      />
    </div>
  );
}
