import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { X, Plus, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import type { KGEntityType, KGRelationType, KGPropertyField } from "@/types/knowledge-graph";

interface KGTypeFormDialogProps {
  open: boolean;
  onClose: () => void;
  onSave: (data: Record<string, unknown>) => Promise<void>;
  existing?: KGEntityType | KGRelationType;
  kind: "entity" | "relation";
}

export function KGTypeFormDialog({ open, onClose, onSave, existing, kind }: KGTypeFormDialogProps) {
  const { t } = useTranslation("memory");
  const [name, setName] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [color, setColor] = useState("#3b82f6");
  const [icon, setIcon] = useState("");
  const [description, setDescription] = useState("");
  const [directed, setDirected] = useState(true);
  const [schema, setSchema] = useState<KGPropertyField[]>([]);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (existing) {
      setName(existing.name);
      setDisplayName(existing.display_name);
      setColor(existing.color || "#3b82f6");
      setDescription(existing.description || "");
      setSchema(existing.properties_schema || []);
      if ("icon" in existing) setIcon((existing as KGEntityType).icon || "");
      if ("directed" in existing) setDirected((existing as KGRelationType).directed);
    } else {
      setName(""); setDisplayName(""); setColor("#3b82f6"); setIcon("");
      setDescription(""); setDirected(true); setSchema([]);
    }
  }, [existing, open]);

  if (!open) return null;

  const handleSave = async () => {
    setSaving(true);
    try {
      const data: Record<string, unknown> = {
        name, display_name: displayName, color, description,
        properties_schema: schema.length > 0 ? schema : undefined,
      };
      if (kind === "entity") data.icon = icon;
      if (kind === "relation") data.directed = directed;
      if (existing) data.id = existing.id;
      await onSave(data);
      onClose();
    } finally {
      setSaving(false);
    }
  };

  const addField = () => setSchema([...schema, { key: "", label: "", type: "string", required: false }]);
  const removeField = (i: number) => setSchema(schema.filter((_, idx) => idx !== i));
  const updateField = (i: number, patch: Partial<KGPropertyField>) => {
    const next = [...schema];
    next[i] = { ...next[i], ...patch } as KGPropertyField;
    setSchema(next);
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50" onClick={onClose}>
      <div className="bg-background border rounded-lg shadow-lg w-full max-w-lg max-h-[90vh] overflow-y-auto" onClick={(e) => e.stopPropagation()}>
        <div className="flex items-center justify-between p-4 border-b">
          <h3 className="text-sm font-semibold">{existing ? t("kg.types.editType", "Edit Type") : t("kg.types.addType", "Add Type")}</h3>
          <Button variant="ghost" size="icon" className="h-6 w-6" onClick={onClose}><X className="h-4 w-4" /></Button>
        </div>
        <div className="p-4 space-y-3">
          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="text-xs text-muted-foreground">{t("kg.types.name", "Name (slug)")}</label>
              <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="person" disabled={existing?.is_system} />
            </div>
            <div>
              <label className="text-xs text-muted-foreground">{t("kg.types.displayName", "Display Name")}</label>
              <Input value={displayName} onChange={(e) => setDisplayName(e.target.value)} placeholder="Person" />
            </div>
          </div>
          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="text-xs text-muted-foreground">{t("kg.types.color", "Color")}</label>
              <div className="flex gap-2 items-center">
                <input type="color" value={color} onChange={(e) => setColor(e.target.value)} className="h-8 w-8 rounded border cursor-pointer" />
                <Input value={color} onChange={(e) => setColor(e.target.value)} className="flex-1" />
              </div>
            </div>
            {kind === "entity" && (
              <div>
                <label className="text-xs text-muted-foreground">{t("kg.types.icon", "Icon")}</label>
                <Input value={icon} onChange={(e) => setIcon(e.target.value)} placeholder="user" />
              </div>
            )}
          </div>
          {kind === "relation" && (
            <div className="flex items-center gap-2">
              <input type="checkbox" checked={directed} onChange={(e) => setDirected(e.target.checked)} className="rounded" />
              <label className="text-xs text-muted-foreground">{t("kg.types.directed", "Directed")}</label>
            </div>
          )}
          <div>
            <label className="text-xs text-muted-foreground">{t("kg.types.description", "Description")}</label>
            <textarea value={description} onChange={(e) => setDescription(e.target.value)} rows={2} className="w-full rounded-md border bg-transparent px-3 py-2 text-sm" />
          </div>
          <div>
            <div className="flex items-center justify-between mb-1">
              <label className="text-xs text-muted-foreground">{t("kg.types.propertiesSchema", "Properties Schema")}</label>
              <Button variant="ghost" size="sm" className="h-6 text-xs" onClick={addField}><Plus className="h-3 w-3 mr-1" />{t("kg.types.addField", "Add Field")}</Button>
            </div>
            {schema.map((f, i) => (
              <div key={i} className="flex gap-1.5 items-center mb-1.5">
                <Input value={f.key} onChange={(e) => updateField(i, { key: e.target.value })} placeholder="key" className="w-20" />
                <Input value={f.label} onChange={(e) => updateField(i, { label: e.target.value })} placeholder="Label" className="w-24" />
                <select value={f.type} onChange={(e) => updateField(i, { type: e.target.value as KGPropertyField["type"] })} className="h-9 rounded-md border bg-transparent px-2 text-xs">
                  <option value="string">string</option><option value="number">number</option><option value="date">date</option><option value="enum">enum</option>
                </select>
                <input type="checkbox" checked={f.required} onChange={(e) => updateField(i, { required: e.target.checked })} className="rounded" title="Required" />
                {f.type === "enum" && (
                  <Input value={(f.enum_values || []).join(",")} onChange={(e) => updateField(i, { enum_values: e.target.value.split(",").map((s) => s.trim()).filter(Boolean) })} placeholder="a,b,c" className="w-24" />
                )}
                <Button variant="ghost" size="icon" className="h-6 w-6 shrink-0" onClick={() => removeField(i)}><Trash2 className="h-3 w-3" /></Button>
              </div>
            ))}
          </div>
        </div>
        <div className="flex justify-end gap-2 p-4 border-t">
          <Button variant="outline" onClick={onClose}>{t("common:cancel", "Cancel")}</Button>
          <Button onClick={handleSave} disabled={!name || saving}>{saving ? "..." : t("common:save", "Save")}</Button>
        </div>
      </div>
    </div>
  );
}
