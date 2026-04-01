import { useState } from "react";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Input } from "@/components/ui/input";
import { useTranslation } from "react-i18next";
import type { KGEntity, KGRelationType } from "@/types/knowledge-graph";

type Direction = "outgoing" | "incoming";

interface KGRelationFormDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  sourceEntityId: string;
  entities: KGEntity[];
  relationTypes: KGRelationType[];
  onSave: (data: { source_entity_id: string; target_entity_id: string; relation_type: string; confidence: number }) => Promise<void>;
}

export function KGRelationFormDialog({ open, onOpenChange, sourceEntityId, entities, relationTypes, onSave }: KGRelationFormDialogProps) {
  const { t } = useTranslation("memory");

  const [direction, setDirection] = useState<Direction>("outgoing");
  const [targetEntityId, setTargetEntityId] = useState("");
  const [relationType, setRelationType] = useState("");
  const [confidence, setConfidence] = useState("1.0");
  const [loading, setLoading] = useState(false);

  // Exclude source entity from target list
  const targetEntities = entities.filter((e) => e.id !== sourceEntityId);

  const handleSubmit = async () => {
    if (!targetEntityId || !relationType) return;
    setLoading(true);
    try {
      const sourceId = direction === "outgoing" ? sourceEntityId : targetEntityId;
      const targetId = direction === "outgoing" ? targetEntityId : sourceEntityId;
      await onSave({
        source_entity_id: sourceId,
        target_entity_id: targetId,
        relation_type: relationType,
        confidence: parseFloat(confidence) || 1.0,
      });
      // Reset form
      setTargetEntityId("");
      setRelationType("");
      setConfidence("1.0");
      setDirection("outgoing");
      onOpenChange(false);
    } finally {
      setLoading(false);
    }
  };

  // Reset form on open
  const handleOpenChange = (v: boolean) => {
    if (v) {
      setTargetEntityId("");
      setRelationType("");
      setConfidence("1.0");
      setDirection("outgoing");
    }
    if (!loading) onOpenChange(v);
  };

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent aria-describedby={undefined} className="max-w-md">
        <DialogHeader>
          <DialogTitle>{t("kg.relationForm.title")}</DialogTitle>
        </DialogHeader>

        <div className="space-y-3 py-2">
          <div className="grid gap-1.5">
            <label className="text-xs font-medium">{t("kg.relationForm.direction")}</label>
            <div className="flex gap-2">
              <Button
                variant={direction === "outgoing" ? "secondary" : "outline"}
                size="sm"
                onClick={() => setDirection("outgoing")}
                className="flex-1"
              >
                {t("kg.relationForm.outgoing")}
              </Button>
              <Button
                variant={direction === "incoming" ? "secondary" : "outline"}
                size="sm"
                onClick={() => setDirection("incoming")}
                className="flex-1"
              >
                {t("kg.relationForm.incoming")}
              </Button>
            </div>
          </div>

          <div className="grid gap-1.5">
            <label className="text-xs font-medium">{t("kg.relationForm.targetEntity")}</label>
            <Select value={targetEntityId} onValueChange={setTargetEntityId}>
              <SelectTrigger className="h-8 text-sm">
                <SelectValue placeholder={t("kg.relationForm.targetEntity")} />
              </SelectTrigger>
              <SelectContent>
                {targetEntities.map((e) => (
                  <SelectItem key={e.id} value={e.id}>
                    {e.name} <span className="text-muted-foreground text-xs">({e.entity_type})</span>
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div className="grid gap-1.5">
            <label className="text-xs font-medium">{t("kg.relationForm.relationType")}</label>
            <Select value={relationType} onValueChange={setRelationType}>
              <SelectTrigger className="h-8 text-sm">
                <SelectValue placeholder={t("kg.relationForm.relationType")} />
              </SelectTrigger>
              <SelectContent>
                {relationTypes.map((rt) => (
                  <SelectItem key={rt.id} value={rt.name}>
                    {rt.display_name || rt.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div className="grid gap-1.5">
            <label className="text-xs font-medium">{t("kg.relationForm.confidence")}</label>
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
            {t("kg.relationForm.cancel")}
          </Button>
          <Button onClick={handleSubmit} disabled={loading || !targetEntityId || !relationType}>
            {loading ? "..." : t("kg.relationForm.add")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
