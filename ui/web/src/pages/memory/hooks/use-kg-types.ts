import { useCallback } from "react";
import { useQuery, useQueryClient, useMutation } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";
import { queryKeys } from "@/lib/query-keys";
import { toast } from "@/stores/use-toast-store";
import i18n from "@/i18n";
import type { KGEntityType, KGRelationType } from "@/types/knowledge-graph";

export function useKgEntityTypes(agentId: string) {
  const http = useHttp();
  const queryClient = useQueryClient();

  const { data, isLoading } = useQuery({
    queryKey: queryKeys.kg.entityTypes(agentId),
    queryFn: async () => {
      if (!agentId) return [];
      return (await http.get<KGEntityType[]>(`/v1/agents/${agentId}/kg/entity-types`)) ?? [];
    },
    enabled: !!agentId,
    placeholderData: (prev) => prev,
  });

  const invalidate = useCallback(
    () => queryClient.invalidateQueries({ queryKey: queryKeys.kg.entityTypes(agentId) }),
    [queryClient, agentId],
  );

  const createType = useMutation({
    mutationFn: async (body: Partial<KGEntityType>) => {
      return http.post<KGEntityType>(`/v1/agents/${agentId}/kg/entity-types`, body);
    },
    onSuccess: () => { invalidate(); toast.success(i18n.t("memory:toast.entityTypeSaved", "Entity type saved")); },
    onError: (err: Error) => toast.error(i18n.t("memory:toast.entityTypeSaveFailed", "Failed to save entity type"), err.message),
  });

  const updateType = useMutation({
    mutationFn: async ({ id, ...body }: Partial<KGEntityType> & { id: string }) => {
      return http.put<KGEntityType>(`/v1/agents/${agentId}/kg/entity-types/${id}`, body);
    },
    onSuccess: () => { invalidate(); toast.success(i18n.t("memory:toast.entityTypeUpdated", "Entity type updated")); },
    onError: (err: Error) => toast.error(i18n.t("memory:toast.entityTypeUpdateFailed", "Failed to update entity type"), err.message),
  });

  const deleteType = useMutation({
    mutationFn: async (id: string) => {
      return http.delete(`/v1/agents/${agentId}/kg/entity-types/${id}`);
    },
    onSuccess: () => { invalidate(); toast.success(i18n.t("memory:toast.entityTypeDeleted", "Entity type deleted")); },
    onError: (err: Error) => toast.error(i18n.t("memory:toast.entityTypeDeleteFailed", "Failed to delete entity type"), err.message),
  });

  return {
    entityTypes: data ?? [],
    loading: isLoading,
    createType: createType.mutateAsync,
    updateType: updateType.mutateAsync,
    deleteType: deleteType.mutateAsync,
    refresh: invalidate,
  };
}

export function useKgRelationTypes(agentId: string) {
  const http = useHttp();
  const queryClient = useQueryClient();

  const { data, isLoading } = useQuery({
    queryKey: queryKeys.kg.relationTypes(agentId),
    queryFn: async () => {
      if (!agentId) return [];
      return (await http.get<KGRelationType[]>(`/v1/agents/${agentId}/kg/relation-types`)) ?? [];
    },
    enabled: !!agentId,
    placeholderData: (prev) => prev,
  });

  const invalidate = useCallback(
    () => queryClient.invalidateQueries({ queryKey: queryKeys.kg.relationTypes(agentId) }),
    [queryClient, agentId],
  );

  const createType = useMutation({
    mutationFn: async (body: Partial<KGRelationType>) => {
      return http.post<KGRelationType>(`/v1/agents/${agentId}/kg/relation-types`, body);
    },
    onSuccess: () => { invalidate(); toast.success(i18n.t("memory:toast.relationTypeSaved", "Relation type saved")); },
    onError: (err: Error) => toast.error(i18n.t("memory:toast.relationTypeSaveFailed", "Failed to save relation type"), err.message),
  });

  const updateType = useMutation({
    mutationFn: async ({ id, ...body }: Partial<KGRelationType> & { id: string }) => {
      return http.put<KGRelationType>(`/v1/agents/${agentId}/kg/relation-types/${id}`, body);
    },
    onSuccess: () => { invalidate(); toast.success(i18n.t("memory:toast.relationTypeUpdated", "Relation type updated")); },
    onError: (err: Error) => toast.error(i18n.t("memory:toast.relationTypeUpdateFailed", "Failed to update relation type"), err.message),
  });

  const deleteType = useMutation({
    mutationFn: async (id: string) => {
      return http.delete(`/v1/agents/${agentId}/kg/relation-types/${id}`);
    },
    onSuccess: () => { invalidate(); toast.success(i18n.t("memory:toast.relationTypeDeleted", "Relation type deleted")); },
    onError: (err: Error) => toast.error(i18n.t("memory:toast.relationTypeDeleteFailed", "Failed to delete relation type"), err.message),
  });

  return {
    relationTypes: data ?? [],
    loading: isLoading,
    createType: createType.mutateAsync,
    updateType: updateType.mutateAsync,
    deleteType: deleteType.mutateAsync,
    refresh: invalidate,
  };
}
