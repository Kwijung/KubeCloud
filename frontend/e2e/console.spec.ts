import { expect, test } from "@playwright/test";
import type { APIRequestContext, Page } from "@playwright/test";

// These tests drive the deployed console the way an operator does. They create their own resources with
// a unique suffix and remove them through the UI; API calls are used only to clean up after a failure.
const project = "platform";
const suffix = String(Date.now() % 100000);

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    window.localStorage.setItem("kloudia-language", "ko");
    window.localStorage.setItem("kloudia-admin-mode", "false");
  });
});

async function confirmDialog(page: Page, button: string) {
  const dialog = page.getByRole("dialog");
  await expect(dialog).toBeVisible();
  await dialog.getByRole("button", { name: button, exact: true }).click();
  await expect(dialog).toBeHidden();
}

test("navigation and admin mode", async ({ page }) => {
  await page.goto("/");
  await expect(page).toHaveURL(/\/projects\/[^/]+$/);
  await expect(page.getByRole("heading", { name: "대시보드" })).toBeVisible();
  await expect(page.getByText("API 정상")).toBeVisible();

  const nav = page.getByRole("navigation", { name: "주 메뉴" });
  await nav.getByRole("link", { name: "가상 머신" }).click();
  await expect(page.getByRole("heading", { name: "가상 머신", exact: true })).toBeVisible();
  await nav.getByRole("link", { name: "이미지" }).click();
  await expect(page.getByRole("heading", { name: "공용 이미지" })).toBeVisible();
  await nav.getByRole("link", { name: "VPC" }).click();
  await expect(page.getByRole("heading", { name: "VPC", exact: true })).toBeVisible();

  // Admin pages appear only in admin mode.
  await expect(nav.getByRole("link", { name: "클러스터" })).toHaveCount(0);
  await page.getByRole("switch", { name: "관리자 모드" }).check({ force: true });
  await nav.getByRole("link", { name: "클러스터" }).click();
  await expect(page.getByRole("heading", { name: "구성 요소 점검" })).toBeVisible();
  await expect(page.getByRole("cell", { name: "kube-ovn-vpc" })).toBeVisible();
  await nav.getByRole("link", { name: "공용 이미지" }).click();
  await expect(page.getByRole("heading", { name: "공용 이미지 등록" })).toBeVisible();
  // Leaving admin mode on an admin page goes back to the project.
  await page.getByRole("switch", { name: "관리자 모드" }).uncheck({ force: true });
  await expect(page).toHaveURL(/\/projects\//);

  // The language switch translates the console.
  await page.getByRole("combobox", { name: "언어" }).selectOption("en");
  await expect(page.getByRole("heading", { name: "Dashboard" })).toBeVisible();
});

test("flavor management", async ({ page, request }) => {
  const name = `ui-${suffix}`;
  test.info().annotations.push({ type: "flavor", description: name });
  await page.goto("/admin/cluster");
  await page.getByRole("button", { name: "사양 추가" }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("이름").fill(name);
  await dialog.getByLabel("vCPU").fill("1");
  await dialog.getByLabel("메모리 (GiB)").fill("0.5");
  await dialog.getByLabel("루트 디스크 (GiB)").fill("3");
  await dialog.getByRole("button", { name: "저장" }).click();
  await expect(dialog).toBeHidden();
  try {
    const row = page.getByRole("row", { name: new RegExp(name) });
    await expect(row).toContainText("512 MiB");
    await expect(row).toContainText("3 GiB");

    // A duplicate name is refused with an explanation.
    await page.getByRole("button", { name: "사양 추가" }).click();
    await dialog.getByLabel("이름").fill(name);
    await dialog.getByRole("button", { name: "저장" }).click();
    await expect(dialog.getByText("같은 이름의 사양이 이미 있습니다")).toBeVisible();
    await dialog.getByRole("button", { name: "취소" }).click();

    // Rename, then hide it from projects.
    await row.getByRole("button", { name: `${name} 편집` }).click();
    await dialog.getByLabel("이름").fill(`${name}-x`);
    await dialog.getByRole("button", { name: "저장" }).click();
    await expect(dialog).toBeHidden();
    const renamed = page.getByRole("row", { name: new RegExp(`${name}-x`) });
    await renamed.getByRole("switch").uncheck({ force: true });
    await expect(renamed.getByText("숨김")).toBeVisible();
    // Hidden flavors are no longer offered to projects.
    await expect
      .poll(async () => ((await (await request.get("/api/v1/flavors")).json()).data as Array<{ name: string }>).map((f) => f.name))
      .not.toContain(`${name}-x`);

    await renamed.getByRole("button", { name: `${name}-x 삭제` }).click();
    await confirmDialog(page, "삭제");
    await expect(renamed).toHaveCount(0);
  } finally {
    await deleteFlavorsNamed(request, name);
  }
});

test("VPC and subnet lifecycle", async ({ page, request }) => {
  const name = `ui${suffix}`;
  // 172.20-29/16: private, and clear of the cluster ranges in KLOUDIA_RESERVED_CIDRS and the API e2e VPCs (10.60-89).
  const octet = 20 + (Number(suffix) % 10);
  await page.goto(`/projects/${project}/vpcs`);
  await page.getByLabel("이름", { exact: true }).fill(name);
  await page.getByLabel("CIDR", { exact: true }).fill(`172.${octet}.0.0/16`);
  await page.getByRole("button", { name: "VPC 만들기" }).click();
  try {
    await expect(page.getByRole("heading", { name })).toBeVisible();
    await expect(page.getByText(`172.${octet}.0.0/16 · 다른 VPC와 격리된 가상 라우터`)).toBeVisible();

    // A subnet outside the VPC range is refused.
    await page.getByRole("button", { name: "서브넷 만들기" }).click();
    const dialog = page.getByRole("dialog");
    await dialog.getByLabel("이름").fill("pub");
    await dialog.getByLabel("CIDR").fill("10.250.1.0/24");
    await dialog.getByLabel("유형").selectOption("public");
    await dialog.getByRole("button", { name: "서브넷 만들기" }).click();
    await expect(dialog.getByText("서브넷 CIDR이 VPC CIDR 밖에 있습니다")).toBeVisible();
    await dialog.getByLabel("CIDR").fill(`172.${octet}.1.0/24`);
    await dialog.getByRole("button", { name: "서브넷 만들기" }).click();
    await expect(dialog).toBeHidden({ timeout: 60000 });
    const row = page.getByRole("row", { name: /pub/ });
    await expect(row).toContainText(`172.${octet}.1.1`);
    await expect(row).toContainText("아웃바운드 허용 (NAT)");

    // A VPC with subnets cannot be deleted.
    await page.getByRole("button", { name: "VPC 삭제" }).click();
    await confirmDialog(page, "삭제");
    await expect(page.getByText("서브넷이 남아 있는 VPC는 삭제할 수 없습니다")).toBeVisible();

    await row.getByRole("button", { name: "pub 삭제" }).click();
    await confirmDialog(page, "삭제");
    await expect(page.getByText("서브넷이 없습니다")).toBeVisible({ timeout: 60000 });
    await page.getByRole("button", { name: "VPC 삭제" }).click();
    await confirmDialog(page, "삭제");
    await expect(page).toHaveURL(new RegExp(`/projects/${project}/vpcs$`));
    await expect(page.getByRole("link", { name, exact: true })).toHaveCount(0);
  } finally {
    await deleteVPC(request, `${project}-${name}`);
  }
});

test("VM lifecycle with console and boot log", async ({ page, browser, request }) => {
  test.setTimeout(20 * 60 * 1000);
  const name = `ui-${suffix}`;
  const vmId = `${project}-${name}`;
  await page.goto(`/projects/${project}/vms/new`);
  await page.getByLabel("가상 머신 이름").fill(name);
  await page.getByRole("radiogroup", { name: /공용 이미지/ }).getByText("CirrOS", { exact: false }).first().click();
  await page.getByRole("radiogroup", { name: "사양" }).getByRole("radio", { name: "Small" }).check();
  await page.getByLabel("사용자 이름").fill("cirros");
  await page.getByLabel("비밀번호").fill("gocubsgo");
  await page.getByRole("button", { name: "가상 머신 만들기" }).click();
  try {
    await expect(page).toHaveURL(new RegExp(`/vms/${vmId}$`));
    await expect(page.getByRole("heading", { name })).toBeVisible();
    await expect(page.locator(".page-title-row").getByText("실행 중")).toBeVisible({ timeout: 6 * 60 * 1000 });

    // The VM appears in the list.
    await page.getByRole("navigation", { name: "주 메뉴" }).getByRole("link", { name: "가상 머신" }).click();
    await page.getByLabel("이름, ID, 이미지로 검색").fill(name);
    await expect(page.getByRole("row", { name: new RegExp(name) })).toContainText("실행 중");
    await page.getByRole("link", { name, exact: true }).click();

    // The boot log fills in without logging in.
    await page.getByRole("tab", { name: "콘솔" }).click();
    const bootLog = page.getByTestId("boot-log");
    await expect(async () => {
      await page.getByRole("button", { name: "새로 고침" }).click();
      await expect(bootLog).toContainText("login as 'cirros'", { timeout: 5000 });
    }).toPass({ timeout: 4 * 60 * 1000 });

    // Log in on the serial console and run a command.
    const terminal = page.getByTestId("terminal");
    await expect(page.getByText("연결됨")).toBeVisible();
    await login(page);
    await terminal.click();
    await page.keyboard.type("echo ui-$((6*7))");
    await page.keyboard.press("Enter");
    await expect(terminal).toContainText("ui-42");
    // Ctrl+C reaches the guest: it interrupts a long sleep.
    await page.keyboard.type("sleep 300; echo not-interrupted");
    await page.keyboard.press("Enter");
    // Wait for the echo of the command line so the shell is running sleep when Ctrl+C arrives.
    await expect(terminal).toContainText("echo not-interrupted");
    await page.waitForTimeout(2000);
    await page.keyboard.press("Control+c");
    await page.keyboard.type("echo after-$((1+1))");
    await page.keyboard.press("Enter");
    await expect(terminal).toContainText("after-2", { timeout: 30000 });

    // A second session sees the console is busy, and taking it over drops the first one.
    const otherContext = await browser.newContext({ baseURL: new URL(page.url()).origin, locale: "ko-KR" });
    await otherContext.addInitScript(() => window.localStorage.setItem("kloudia-language", "ko"));
    const other = await otherContext.newPage();
    await other.goto(`/projects/${project}/vms/${vmId}/console`);
    await expect(other.getByText("콘솔이 다른 곳에서 열려 있습니다")).toBeVisible();
    await other.getByRole("button", { name: "기존 세션 끊고 연결" }).click();
    await expect(other.locator(".card-title").getByText("연결됨")).toBeVisible();
    await expect(page.getByText("다른 세션이 콘솔을 가져갔습니다")).toBeVisible();
    await otherContext.close();
    // Reconnecting is the replaced session's own choice.
    await page.getByRole("button", { name: "다시 연결" }).click();
    await expect(page.locator(".card-title").getByText("연결됨")).toBeVisible();

    // Restart from the header, then delete through the confirmation dialog.
    await page.getByRole("button", { name: "다시 시작", exact: true }).click();
    await confirmDialog(page, "다시 시작");
    // The restart replaces the VM instance, which ends the console session.
    await expect(page.locator(".card-title").getByText("연결 끊김")).toBeVisible({ timeout: 2 * 60 * 1000 });
    await expect(page.locator(".page-title-row").getByText("실행 중")).toBeVisible({ timeout: 6 * 60 * 1000 });
    await page.getByRole("button", { name: "삭제", exact: true }).click();
    await confirmDialog(page, "삭제");
    await expect(page).toHaveURL(new RegExp(`/projects/${project}/vms$`));
  } finally {
    await request.delete(`/api/v1/projects/${project}/vms/${vmId}`);
  }
});

// login waits for the CirrOS login prompt in the terminal and signs in.
async function login(page: Page) {
  const terminal = page.getByTestId("terminal");
  await terminal.click();
  await expect(async () => {
    await page.keyboard.press("Enter");
    await expect(terminal).toContainText(/login:|\$ $/m, { timeout: 5000 });
  }).toPass({ timeout: 3 * 60 * 1000 });
  if ((await terminal.textContent())?.trimEnd().endsWith("$")) {
    return;
  }
  await page.keyboard.type("cirros");
  await page.keyboard.press("Enter");
  await expect(terminal).toContainText("Password:");
  await page.keyboard.type("gocubsgo");
  await page.keyboard.press("Enter");
  await expect(terminal).toContainText("$");
}

async function deleteFlavorsNamed(request: APIRequestContext, prefix: string) {
  const response = await request.get("/api/v1/admin/flavors");
  const flavors = ((await response.json()).data ?? []) as Array<{ id: string; name: string }>;
  for (const flavor of flavors.filter((f) => f.name.startsWith(prefix))) {
    await request.delete(`/api/v1/admin/flavors/${flavor.id}`);
  }
}

async function deleteVPC(request: APIRequestContext, networkId: string) {
  const base = `/api/v1/projects/${project}/networks/${networkId}`;
  const subnets = await request.get(`${base}/subnets`);
  if (subnets.ok()) {
    for (const subnet of ((await subnets.json()).data ?? []) as Array<{ id: string }>) {
      await request.delete(`${base}/subnets/${subnet.id}`);
    }
  }
  await request.delete(base);
}
