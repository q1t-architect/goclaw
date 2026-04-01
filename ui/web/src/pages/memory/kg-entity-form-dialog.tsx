import { useState, useEffect, useMemo } from "react";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { useTranslation } from "react-i18next";
import type { KGEntity, KGEntityType } from "@/types/knowledge-graph";

function generateExternalId(name: string): string {
  return name
    .toLowerCase()
    .replace(/\s+/g, "-")
    .replace(/[^a-z0-9-]/g, "")
    .replace(/-+/g, "-")
    .replace(/^-|-$/g, "")
    || "entity";
}

interface KGEntityFormDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  agentId: string;
  userId?: string;
  entity: KGEntity | null; // null = create, entity = edit
  entityTypes: KGEntityType[];
  onSave: (data: Record<string, unknown>) => Promise<void>;
}

export function KGEntityFormDialog({ open, onOpenChange, userId, entity, entityTypes, onSave }: KGEntityFormDialogProps) {
  const { t } = useTranslation("memory");
  const isEdit = !!entity;

  const [name, setName] = useState("");
  const [externalId, setExternalId] = useState("");
  const [entityType, setEntityType] = useState("");
  const [description, setDescription] = useState("");
  const [confidence, setConfidence] = useState("1.0");
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (open) {
      if (entity) {
        setName(entity.name);
        setExternalId(entity.external_id);
        setEntityType(entity.entity_type);
        setDescription(entity.description || "");
        setConfidence(String(entity.confidence));
      } else {
        setName("");
        setExternalId("");
        setEntityType("");
        setDescription("");
        setConfidence("1.0");
      }
    }
  }, [open, entity]);

  const autoExternalId = useMemo(() => generateExternalId(name), [name]);

  const handleSubmit = async () => {
    if (!name.trim() || !entityType) return;
    setLoading(true);
    try {
      if (isEdit) {
        await onSave({
          name: name.trim(),
          entity_type: entityType,
          description: description.trim() || undefined,
          confidence: parseFloat(confidence) || 1.0,
        });
      } else {
        await onSave({
          external_id: externalId || autoExternalId,
          name: name.trim(),
          entity_type: entityType,
          description: description.trim() || undefined,
          confidence: parseFloat(confidence) || 1.0,
          user_id: userId || "",
        });
      }
      onOpenChange(false);
    } finally {
      setLoading(false);
    }
  };

  const title = isEdit ? t("kg.entityForm.titleEdit") : t("kg.entityForm.titleCreate");

  return (
    <Dialog open={open} onOpenChange={(v) => !loading && onOpenChange(v)}>
      <DialogContent aria-describedby={undefined} className="max-w-md">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
        </DialogHeader>

        <div className="space-y-3 py-2">
          <div className="grid gap-1.5">
            <label className="text-xs font-medium">{t("kg.entityForm.name")}</label>
            <Input
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="Entity name"
              className="h-8 text-sm"
            />
          </div>

          <div className="grid gap-1.5">
            <label className="text-xs font-medium">{t("kg.entityForm.externalId")}</label>
            <Input
              value={isEdit ? externalId : (externalId || autoExternalId)}
              onChange={(e) => setExternalId(e.target.value)}
              disabled={isEdit}
              placeholder="auto-generated"
              className="h-8 text-sm font-mono"
            />
          </div>

          <div className="grid gap-1.5">
            <label className="text-xs font-medium">{t("kg.entityForm.entityType")}</label>
            <Select value={entityType} onValueChange={setEntityType}>
              <SelectTrigger className="h-8 text-sm">
                <SelectValue placeholder={t("kg.entityForm.entityType")} />
              </SelectTrigger>
              <SelectContent>
                {entityTypes.map((et) => (
                  <SelectItem key={et.id} value={et.name}>
                    {et.display_name || et.name}
                  </SelectItem>
                ))}
                {/* Fallback for current value not in types list */}
                {entityType && !entityTypes.some(et => et.name === entityType) && (
                  <SelectItem value={entityType}>{entityType}</SelectItem>
                )}
              </SelectContent>
            </Select>
          </div>

          <div className="grid gap-1.5">
            <label className="text-xs font-medium">{t("kg.entityForm.description")}</label>
            <Textarea
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="Optional description"
              rows={3}
              className="text-sm"
            />
          </div>

          <div className="grid gap-1.5">
            <label className="text-xs font-medium">{t("kg.entityForm.confidence")}</label>
            <Input
              type="number"
              min={0}
              max={1}
              step={0.1}
              value={confidence}
              onChange={(e) => setConfidence(e.target.value)}
              className="h-8 text-sm w-24"
            />
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={loading}>
            {t("kg.entityForm.cancel")}
          </Button>
          <Button onClick={handleSubmit} disabled={loading || !name.trim() || !entityType}>
            {loading ? "..." : isEdit ? t("kg.entityForm.save") : t("kg.entityForm.create")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
