import { Plus, Trash2 } from "lucide-react";
import type { FormEvent, ReactNode } from "react";
import { useState } from "react";
import type { CreateImageRequest, Image } from "../api";
import { fill, formatBytes } from "../format";
import { statusTone, useI18n } from "../i18n";
import { Alert, Badge, Button, Card, Empty, Field, IconButton, Input, Select, Spinner, useConfirm } from "../ui";

// Public cloud images that boot with cloud-init on KubeVirt.
export const imagePresets = [
  { name: "CirrOS 0.6.2", url: "https://download.cirros-cloud.net/0.6.2/cirros-0.6.2-x86_64-disk.img" },
  { name: "Ubuntu 24.04", url: "https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.img" },
  { name: "Ubuntu 22.04", url: "https://cloud-images.ubuntu.com/jammy/current/jammy-server-cloudimg-amd64.img" },
  { name: "Debian 12", url: "https://cloud.debian.org/images/cloud/bookworm/latest/debian-12-genericcloud-amd64.qcow2" },
  { name: "Rocky Linux 9", url: "https://dl.rockylinux.org/pub/rocky/9/images/x86_64/Rocky-9-GenericCloud-Base.latest.x86_64.qcow2" },
] as const;

export function ImageRegisterForm({
  title,
  description,
  submitLabel,
  pending,
  error,
  onSubmit,
}: {
  title: string;
  description: string;
  submitLabel: string;
  pending: boolean;
  error: unknown;
  onSubmit: (image: CreateImageRequest, reset: () => void) => void;
}) {
  const { t, errorText } = useI18n();
  const [preset, setPreset] = useState("");
  const [name, setName] = useState("");
  const [url, setUrl] = useState("");
  const [osType, setOsType] = useState("linux");
  const valid = name.trim() !== "" && url.trim() !== "";

  function choosePreset(value: string) {
    setPreset(value);
    const chosen = imagePresets.find((item) => item.name === value);
    if (chosen) {
      setName(chosen.name);
      setUrl(chosen.url);
      setOsType("linux");
    }
  }

  function submit(event: FormEvent) {
    event.preventDefault();
    if (valid && !pending) {
      onSubmit({ name: name.trim(), sourceUrl: url.trim(), osType }, () => {
        setPreset("");
        setName("");
        setUrl("");
      });
    }
  }

  return (
    <Card title={title} description={description}>
      <form className="stack" onSubmit={submit}>
        <div className="form-grid">
          <Field label={t.imgPreset}>
            {(id) => (
              <Select id={id} value={preset} onChange={(event) => choosePreset(event.target.value)}>
                <option value="">{t.imgPresetCustom}</option>
                {imagePresets.map((item) => (
                  <option key={item.name} value={item.name}>
                    {item.name}
                  </option>
                ))}
              </Select>
            )}
          </Field>
          <Field label={t.imgName}>
            {(id) => <Input id={id} value={name} placeholder={t.imgNamePlaceholder} onChange={(event) => setName(event.target.value)} />}
          </Field>
          <Field label={t.imgOs}>
            {(id) => (
              <Select id={id} value={osType} onChange={(event) => setOsType(event.target.value)}>
                <option value="linux">Linux</option>
                <option value="windows">Windows</option>
              </Select>
            )}
          </Field>
        </div>
        <Field label={t.imgUrl}>
          {(id) => <Input id={id} value={url} placeholder={t.imgUrlPlaceholder} onChange={(event) => setUrl(event.target.value)} />}
        </Field>
        <Alert tone="info">{t.imgRegisterNote}</Alert>
        {Boolean(error) && <Alert tone="error">{errorText(error)}</Alert>}
        <div className="form-actions">
          <Button type="submit" variant="primary" icon={Plus} disabled={!valid} loading={pending}>
            {submitLabel}
          </Button>
        </div>
      </form>
    </Card>
  );
}

export function ImageTable({
  images,
  loading,
  empty,
  usageLabel,
  usage,
  onDelete,
  deleting,
}: {
  images: Image[];
  loading: boolean;
  empty: string;
  usageLabel?: string;
  usage?: (image: Image) => ReactNode;
  onDelete?: (image: Image) => void;
  deleting?: string;
}) {
  const { t, statusText } = useI18n();
  const confirm = useConfirm();
  if (loading) {
    return <Empty title={<Spinner label={t.loading} />} />;
  }
  if (images.length === 0) {
    return <Empty title={empty} />;
  }

  async function remove(image: Image) {
    const ok = await confirm({
      title: t.imgDeleteConfirmTitle,
      message: fill(t.imgDeleteConfirm, { name: image.name }),
      confirmLabel: t.delete,
      cancelLabel: t.cancel,
      danger: true,
    });
    if (ok) {
      onDelete?.(image);
    }
  }

  return (
    <div className="table-wrap">
      <table className="table">
        <thead>
          <tr>
            <th>{t.name}</th>
            <th>{t.status}</th>
            <th>{t.imgOs}</th>
            <th>{t.imgSize}</th>
            <th>{t.imgMinDisk}</th>
            {usageLabel && <th>{usageLabel}</th>}
            {onDelete && (
              <th className="col-actions">
                <span className="muted">{t.actions}</span>
              </th>
            )}
          </tr>
        </thead>
        <tbody>
          {images.map((image) => (
            <tr key={image.id}>
              <td>
                <div className="cell-main">
                  <span>{image.name}</span>
                  <span className="cell-sub">{image.sourceUrl}</span>
                </div>
              </td>
              <td>
                <div className="cell-main">
                  <span>
                    <Badge tone={statusTone(image.status)} dot>
                      {statusText(image.status)}
                    </Badge>
                  </span>
                  {image.status === "Failed" && image.statusMessage && (
                    <span className="cell-sub" style={{ color: "var(--c-error)" }}>
                      {fill(t.imgFailedReason, { reason: image.statusMessage })}
                    </span>
                  )}
                </div>
              </td>
              <td>{image.osType}</td>
              <td>{image.virtualSizeBytes ? formatBytes(image.virtualSizeBytes) : <span className="muted">{t.imgSizeUnknown}</span>}</td>
              <td>{image.minRootDiskBytes ? formatBytes(image.minRootDiskBytes) : "—"}</td>
              {usageLabel && <td>{usage?.(image)}</td>}
              {onDelete && (
                <td className="col-actions">
                  <IconButton
                    variant="danger"
                    icon={Trash2}
                    label={fill(t.imgDelete, { name: image.name })}
                    disabled={deleting === image.id}
                    onClick={() => remove(image)}
                  />
                </td>
              )}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
