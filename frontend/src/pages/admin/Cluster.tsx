import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Pencil, Plus, RefreshCw, Trash2 } from "lucide-react";
import type { FormEvent } from "react";
import { useState } from "react";
import { api } from "../../api";
import type { Flavor } from "../../api";
import { keys } from "../../app/queries";
import { bytesToGib, fill, formatBytes, gibToBytes } from "../../format";
import { statusTone, useI18n } from "../../i18n";
import { Alert, Badge, Button, Card, Empty, Field, IconButton, Input, Modal, PageHeader, Spinner, Switch, useConfirm } from "../../ui";

export function Cluster() {
  const { t, statusText, errorText } = useI18n();
  const queryClient = useQueryClient();
  const confirm = useConfirm();
  const preflight = useQuery({ queryKey: keys.preflight, queryFn: api.preflight, refetchInterval: 15000 });
  const flavors = useQuery({ queryKey: keys.adminFlavors, queryFn: api.admin.flavors });
  const [editing, setEditing] = useState<Flavor | "new" | null>(null);

  const invalidateFlavors = () =>
    Promise.all([
      queryClient.invalidateQueries({ queryKey: keys.adminFlavors }),
      queryClient.invalidateQueries({ queryKey: keys.flavors }),
    ]);
  const toggle = useMutation({
    mutationFn: (flavor: Flavor) => api.admin.updateFlavor(flavor.id, { enabled: !flavor.enabled }),
    onSettled: invalidateFlavors,
  });
  const remove = useMutation({
    mutationFn: (flavor: Flavor) => api.admin.deleteFlavor(flavor.id),
    onSettled: invalidateFlavors,
  });

  async function removeFlavor(flavor: Flavor) {
    const ok = await confirm({
      title: fill(t.adminFlavorDelete, { name: flavor.name }),
      message: fill(t.adminFlavorDeleteConfirm, { name: flavor.name }),
      confirmLabel: t.delete,
      cancelLabel: t.cancel,
      danger: true,
    });
    if (ok) {
      remove.mutate(flavor);
    }
  }

  const actionError = toggle.error ?? remove.error;

  return (
    <>
      <PageHeader
        title={t.adminClusterTitle}
        subtitle={t.adminClusterSubtitle}
        badge={
          preflight.data && (
            <Badge tone={statusTone(preflight.data.status)} dot>
              {statusText(preflight.data.status)}
            </Badge>
          )
        }
        actions={
          <Button icon={RefreshCw} loading={preflight.isFetching} onClick={() => preflight.refetch()}>
            {t.adminRecheck}
          </Button>
        }
      />

      <Card title={t.adminChecks} description={t.adminChecksDesc} flush>
        {preflight.isLoading ? (
          <Empty title={<Spinner label={t.loading} />} />
        ) : preflight.isError ? (
          <div style={{ padding: 16 }}>
            <Alert tone="error">{errorText(preflight.error)}</Alert>
          </div>
        ) : (
          <div className="table-wrap">
            <table className="table">
              <thead>
                <tr>
                  <th>{t.adminCheckItem}</th>
                  <th>{t.status}</th>
                  <th>{t.adminCheckValue}</th>
                </tr>
              </thead>
              <tbody>
                {(preflight.data?.checks ?? []).map((check) => (
                  <tr key={check.name}>
                    <td className="mono">{check.name}</td>
                    <td>
                      <Badge tone={statusTone(check.status)} dot>
                        {statusText(check.status)}
                      </Badge>
                    </td>
                    <td className="muted" style={{ overflowWrap: "anywhere" }}>
                      {check.message}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>

      <Card
        title={t.adminFlavors}
        description={t.adminFlavorsDesc}
        actions={
          <Button icon={Plus} onClick={() => setEditing("new")}>
            {t.adminFlavorAdd}
          </Button>
        }
        flush
      >
        {actionError && (
          <div style={{ padding: "12px 16px 0" }}>
            <Alert tone="error">{errorText(actionError)}</Alert>
          </div>
        )}
        {flavors.isLoading ? (
          <Empty title={<Spinner label={t.loading} />} />
        ) : (flavors.data ?? []).length === 0 ? (
          <Empty title={t.adminFlavorEmpty} />
        ) : (
          <div className="table-wrap">
            <table className="table">
              <thead>
                <tr>
                  <th>{t.adminFlavorName}</th>
                  <th>{t.vcpu}</th>
                  <th>{t.memory}</th>
                  <th>{t.createRootDisk}</th>
                  <th>{t.adminFlavorInUse}</th>
                  <th>{t.adminFlavorToggle}</th>
                  <th className="col-actions">
                    <span className="muted">{t.actions}</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {(flavors.data ?? []).map((flavor) => {
                  // Show the new visibility while the change is saved and the list reloads.
                  const enabled = toggle.isPending && toggle.variables?.id === flavor.id ? !flavor.enabled : flavor.enabled;
                  return (
                  <tr key={flavor.id}>
                    <td>
                      <div className="cell-main">
                        <span>{flavor.name}</span>
                        <span className="cell-sub mono">{flavor.id}</span>
                      </div>
                    </td>
                    <td>{flavor.cpu}</td>
                    <td>{formatBytes(flavor.memoryBytes)}</td>
                    <td>{formatBytes(flavor.rootDiskBytes)}</td>
                    <td>{flavor.vmCount ?? 0}</td>
                    <td>
                      <Switch
                        checked={enabled}
                        onChange={() => !toggle.isPending && toggle.mutate(flavor)}
                        label={enabled ? t.adminFlavorEnabled : t.adminFlavorDisabled}
                      />
                    </td>
                    <td className="col-actions">
                      <IconButton icon={Pencil} label={fill(t.adminFlavorEdit, { name: flavor.name })} onClick={() => setEditing(flavor)} />
                      <IconButton
                        variant="danger"
                        icon={Trash2}
                        label={fill(t.adminFlavorDelete, { name: flavor.name })}
                        disabled={(flavor.vmCount ?? 0) > 0 || (remove.isPending && remove.variables?.id === flavor.id)}
                        onClick={() => removeFlavor(flavor)}
                      />
                    </td>
                  </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </Card>

      {editing && (
        <FlavorDialog
          flavor={editing === "new" ? undefined : editing}
          onClose={() => setEditing(null)}
          onSaved={() => {
            setEditing(null);
            void invalidateFlavors();
          }}
        />
      )}
    </>
  );
}

function FlavorDialog({ flavor, onClose, onSaved }: { flavor?: Flavor; onClose: () => void; onSaved: () => void }) {
  const { t, errorText } = useI18n();
  const [name, setName] = useState(flavor?.name ?? "");
  const [cpu, setCpu] = useState(String(flavor?.cpu ?? 1));
  const [memory, setMemory] = useState(flavor ? bytesToGib(flavor.memoryBytes) : "1");
  const [disk, setDisk] = useState(flavor ? bytesToGib(flavor.rootDiskBytes) : "10");
  // A flavor in use keeps its size: running VMs were sized by it.
  const locked = (flavor?.vmCount ?? 0) > 0;
  const save = useMutation({
    mutationFn: () => {
      const input = { name: name.trim(), cpu: Number(cpu), memoryBytes: gibToBytes(memory), rootDiskBytes: gibToBytes(disk) };
      if (!flavor) {
        return api.admin.createFlavor(input);
      }
      return api.admin.updateFlavor(flavor.id, locked ? { name: input.name } : input);
    },
    onSuccess: onSaved,
  });
  const valid = name.trim() !== "" && Number(cpu) >= 1 && Number(memory) > 0 && Number(disk) > 0;

  function submit(event: FormEvent) {
    event.preventDefault();
    if (valid && !save.isPending) {
      save.mutate();
    }
  }

  return (
    <Modal
      title={flavor ? fill(t.adminFlavorEdit, { name: flavor.name }) : t.adminFlavorAdd}
      onClose={onClose}
      footer={
        <>
          <Button onClick={onClose}>{t.cancel}</Button>
          <Button type="submit" form="flavor-form" variant="primary" disabled={!valid} loading={save.isPending}>
            {t.save}
          </Button>
        </>
      }
    >
      <form id="flavor-form" className="stack" onSubmit={submit}>
        {locked && <Alert tone="info">{t.adminFlavorInUseNote}</Alert>}
        <Field label={t.adminFlavorName}>
          {(id) => <Input id={id} value={name} placeholder="medium" onChange={(event) => setName(event.target.value)} />}
        </Field>
        <div className="form-grid">
          <Field label={t.adminFlavorCpu}>
            {(id) => <Input id={id} type="number" min={1} step={1} value={cpu} disabled={locked} onChange={(event) => setCpu(event.target.value)} />}
          </Field>
          <Field label={t.adminFlavorMemory}>
            {(id) => (
              <Input id={id} type="number" min={0.25} step={0.25} value={memory} disabled={locked} onChange={(event) => setMemory(event.target.value)} />
            )}
          </Field>
          <Field label={t.adminFlavorDisk}>
            {(id) => <Input id={id} type="number" min={1} step={1} value={disk} disabled={locked} onChange={(event) => setDisk(event.target.value)} />}
          </Field>
        </div>
        {save.isError && <Alert tone="error">{errorText(save.error)}</Alert>}
      </form>
    </Modal>
  );
}
