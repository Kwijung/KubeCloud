import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { CreateImageRequest, Image } from "../api";
import { projectPath, useProject } from "../app/Layout";
import { keys, useImages, useVMs } from "../app/queries";
import { fill } from "../format";
import { useI18n } from "../i18n";
import { Alert, Badge, Card, PageHeader } from "../ui";
import { ImageRegisterForm, ImageTable } from "./imageParts";

export function Images() {
  const { t, errorText } = useI18n();
  const { projectId, projectName } = useProject();
  const queryClient = useQueryClient();
  const images = useImages(projectId);
  const vms = useVMs(projectId);
  const invalidate = () => queryClient.invalidateQueries({ queryKey: keys.images(projectId) });
  const create = useMutation({
    mutationFn: ({ image }: { image: CreateImageRequest; reset: () => void }) => api.createImage(projectId, image),
    onSuccess: (_, { reset }) => {
      reset();
      void invalidate();
    },
  });
  const remove = useMutation({
    mutationFn: (image: Image) => api.deleteImage(projectId, image.id),
    onSettled: () => invalidate(),
  });

  const all = images.data ?? [];
  const projectImages = all.filter((image) => image.scope === "project");
  const publicImages = all.filter((image) => image.scope === "public");
  // VM counts in this project; the API only counts across projects for admins.
  const used = (image: Image) => (vms.data ?? []).filter((vm) => vm.imageId === image.id).length;

  return (
    <>
      <PageHeader
        crumbs={[{ label: projectName, to: projectPath(projectId) }, { label: t.navImages }]}
        title={t.imgTitle}
        subtitle={t.imgSubtitle}
      />
      {images.isError && <Alert tone="error">{errorText(images.error)}</Alert>}
      <Card title={t.imgProject} description={fill(t.imgProjectDesc, { project: projectName, n: projectImages.length })} flush>
        {remove.isError && (
          <div style={{ padding: "12px 16px 0" }}>
            <Alert tone="error">{errorText(remove.error)}</Alert>
          </div>
        )}
        <ImageTable
          images={projectImages}
          loading={images.isLoading}
          empty={t.imgProjectEmpty}
          usageLabel={t.imgInUse}
          usage={used}
          onDelete={(image) => remove.mutate(image)}
          deleting={remove.isPending ? remove.variables?.id : undefined}
        />
      </Card>
      <Card
        title={t.imgPublic}
        description={fill(t.imgPublicDesc, { n: publicImages.length })}
        actions={<Badge>{t.imgAllProjects}</Badge>}
        flush
      >
        <ImageTable images={publicImages} loading={images.isLoading} empty={t.imgPublicEmpty} usageLabel={t.imgInUse} usage={used} />
      </Card>
      <ImageRegisterForm
        title={t.imgRegisterProject}
        description={fill(t.imgRegisterProjectDesc, { project: projectName })}
        submitLabel={t.imgRegister}
        pending={create.isPending}
        error={create.error}
        onSubmit={(image, reset) => create.mutate({ image, reset })}
      />
    </>
  );
}
