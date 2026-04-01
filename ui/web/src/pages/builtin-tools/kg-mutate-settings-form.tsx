import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import {
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Loader2 } from "lucide-react";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";

interface KGMutateSettings {
  max_entities_per_run: number;
  max_relations_per_run: number;
  allowed_entity_types: string;
  allowed_relation_types: string;
}

const defaultSettings: KGMutateSettings = {
  max_entities_per_run: 10,
  max_relations_per_run: 20,
  allowed_entity_types: "",
  allowed_relation_types: "",
};

interface Props {
  initialSettings: Record<string, unknown>;
  onSave: (settings: Record<string, unknown>) => Promise<void>;
  onCancel: () => void;
}

export function KGMutateSettingsForm({ initialSettings, onSave, onCancel }: Props) {
  const { t } = useTranslation("tools");
  const [settings, setSettings] = useState<KGMutateSettings>(defaultSettings);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    setSettings({
      ...defaultSettings,
      ...initialSettings,
      max_entities_per_run: Number(initialSettings.max_entities_per_run) || defaultSettings.max_entities_per_run,
      max_relations_per_run: Number(initialSettings.max_relations_per_run) || defaultSettings.max_relations_per_run,
      allowed_entity_types: String(initialSettings.allowed_entity_types || ""),
      allowed_relation_types: String(initialSettings.allowed_relation_types || ""),
    });
  }, [initialSettings]);

  const handleSave = async () => {
    setSaving(true);
    try {
      await onSave(settings as unknown as Record<string, unknown>);
    } catch {
      // toast shown by hook
    } finally {
      setSaving(false);
    }
  };

  return (
    <>
      <DialogHeader>
        <DialogTitle>{t("builtin.kgMutateSettings.title")}</DialogTitle>
        <DialogDescription>
          {t("builtin.kgMutateSettings.description")}
        </DialogDescription>
      </DialogHeader>

      <div className="space-y-4 py-2">
        {/* Rate limits */}
        <div className="grid grid-cols-2 gap-4">
          <div className="grid gap-1.5">
            <Label htmlFor="kg-mutate-max-entities" className="text-sm">
              {t("builtin.kgMutateSettings.maxEntitiesPerRun")}
            </Label>
            <Input
              id="kg-mutate-max-entities"
              type="number"
              min={1}
              max={100}
              value={settings.max_entities_per_run}
              onChange={(e) =>
                setSettings((s) => ({ ...s, max_entities_per_run: Number(e.target.value) || 10 }))
              }
              className="max-w-[120px]"
            />
            <p className="text-xs text-muted-foreground">
              {t("builtin.kgMutateSettings.maxEntitiesPerRunHint")}
            </p>
          </div>

          <div className="grid gap-1.5">
            <Label htmlFor="kg-mutate-max-relations" className="text-sm">
              {t("builtin.kgMutateSettings.maxRelationsPerRun")}
            </Label>
            <Input
              id="kg-mutate-max-relations"
              type="number"
              min={1}
              max={100}
              value={settings.max_relations_per_run}
              onChange={(e) =>
                setSettings((s) => ({ ...s, max_relations_per_run: Number(e.target.value) || 20 }))
              }
              className="max-w-[120px]"
            />
            <p className="text-xs text-muted-foreground">
              {t("builtin.kgMutateSettings.maxRelationsPerRunHint")}
            </p>
          </div>
        </div>

        {/* Type whitelists */}
        <div className="grid gap-1.5">
          <Label htmlFor="kg-mutate-entity-types" className="text-sm">
            {t("builtin.kgMutateSettings.allowedEntityTypes")}
          </Label>
          <Textarea
            id="kg-mutate-entity-types"
            value={settings.allowed_entity_types}
            onChange={(e) =>
              setSettings((s) => ({ ...s, allowed_entity_types: e.target.value }))
            }
            placeholder={t("builtin.kgMutateSettings.allowedEntityTypesPlaceholder")}
            rows={2}
            className="text-sm"
          />
          <p className="text-xs text-muted-foreground">
            {t("builtin.kgMutateSettings.allowedEntityTypesHint")}
          </p>
        </div>

        <div className="grid gap-1.5">
          <Label htmlFor="kg-mutate-relation-types" className="text-sm">
            {t("builtin.kgMutateSettings.allowedRelationTypes")}
          </Label>
          <Textarea
            id="kg-mutate-relation-types"
            value={settings.allowed_relation_types}
            onChange={(e) =>
              setSettings((s) => ({ ...s, allowed_relation_types: e.target.value }))
            }
            placeholder={t("builtin.kgMutateSettings.allowedRelationTypesPlaceholder")}
            rows={2}
            className="text-sm"
          />
          <p className="text-xs text-muted-foreground">
            {t("builtin.kgMutateSettings.allowedRelationTypesHint")}
          </p>
        </div>
      </div>

      <DialogFooter>
        <Button variant="outline" onClick={onCancel}>
          {t("builtin.kgMutateSettings.cancel")}
        </Button>
        <Button onClick={handleSave} disabled={saving}>
          {saving && <Loader2 className="h-4 w-4 animate-spin" />}
          {saving ? t("builtin.kgMutateSettings.saving") : t("builtin.kgMutateSettings.save")}
        </Button>
      </DialogFooter>
    </>
  );
}
