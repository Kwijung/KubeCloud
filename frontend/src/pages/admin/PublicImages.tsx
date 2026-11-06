import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Search } from "lucide-react";
import { useState } from "react";
import { api } from "../../api";
import type { CreateImageRequest, Image } from "../../api";
import { BUSY_REFRESH_MS, keys } from "../../app/queries";
import { fill } from "../../format";
import { useI18n } from "../../i18n";
import { Alert, Card, Input, PageHeader } from "../../ui";
import { ImageRegisterForm, ImageTable } from "../imageParts";

export function PublicImages() {
  const { t, errorText } = useI18n();
  const queryClient = useQueryClient();
  const [search, setSearch] = useState("");
  const images = useQuery({
    queryKey: keys.adminImages,
    queryFn: api.admin.images,
    refetchInterval: (query) => ((query.state.data ?? []).some((image: Image) => image.status === "Importing") ? BUSY_REFRESH_MS : false),
  });
  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: keys.adminImages });
    // Every project lists public images.
    void queryClient.invalidateQueries({ queryKey: ["images"] });
  };
  const create = useMutation({
    mutationFn: ({ image }: { image: CreateImageRequest; reset: () => void }) => api.admin.createImage(image),
    onSuccess: (_, { reset }) => {
      reset();
      invalidate();
    },
  });
  const remove = useMutation({
    mutationFn: (image: Image) => api.admin.deleteImage(image.id),
    onSettled: invalidate,
  });

  const all = images.data ?? [];
  const query = search.trim().toLowerCase();
  const visible = all.filter((image) => !query || image.name.toLowerCase().includes(query));

  return (
    <>
      <PageHeader title={t.adminPublicTitle} subtitle={t.adminPublicSubtitle} />
      {images.isError && <Alert tone="error">{errorText(images.error)}</Alert>}
      <Card flush>
        <div className="toolbar">
          <div className="toolbar-search row" style={{ flexWrap: "nowrap" }}>
            <Search size={16} className="muted" aria-hidden />
            <Input
              type="search"
              aria-label={t.adminPublicSearch}
              placeholder={t.adminPublicSearch}
              value={search}
              onChange={(event) => setSearch(event.target.value)}
            />
          </div>
          <span className="toolbar-spacer" />
          <span className="muted small">{fill(t.totalCount, { n: all.length })}</span>
        </div>
        {remove.isError && (
          <div style={{ padding: "12px 16px 0" }}>
            <Alert tone="error">{errorText(remove.error)}</Alert>
          </div>
        )}
        <ImageTable
          images={visible}
          loading={images.isLoading}
          empty={t.imgPublicEmpty}
          usageLabel={t.adminPublicInUse}
          usage={(image) => image.vmCount ?? 0}
          onDelete={(image) => remove.mutate(image)}
          deleting={remove.isPending ? remove.variables?.id : undefined}
        />
      </Card>
      <ImageRegisterForm
        title={t.imgRegisterPublic}
        description={t.imgRegisterPublicDesc}
        submitLabel={t.imgRegisterPublic}
        pending={create.isPending}
        error={create.error}
        onSubmit={(image, reset) => create.mutate({ image, reset })}
      />
    </>
  );
}
