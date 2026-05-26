import { test, expect } from '@playwright/test';
import type { Page } from '@playwright/test';

const ACCOUNT_RESPONSE = {
  routing_mode: 'mixed',
  load_balance: 'round_robin',
  counts: { total: 2, enabled: 2, china: 1, international: 1 },
  accounts: [
    {
      id: 'china-1',
      label: 'CN Main',
      region: 'china',
      enabled: true,
      user_id: 'u-cn',
      machine_id: 'm-cn',
      source: 'project_bootstrap',
      token_expire_time: 0,
      loaded_at: new Date().toISOString(),
      has_cosy_key: true,
      has_encrypt_info: true,
      has_access_token: false,
      has_refresh_token: false,
      token_expired: false,
      updated_at: new Date().toISOString(),
    },
    {
      id: 'intl-1',
      label: 'Global Main',
      region: 'international',
      enabled: true,
      user_id: 'u-intl',
      machine_id: '',
      source: 'manual',
      token_expire_time: 0,
      loaded_at: new Date().toISOString(),
      has_cosy_key: false,
      has_encrypt_info: false,
      has_access_token: true,
      has_refresh_token: true,
      token_expired: false,
      updated_at: new Date().toISOString(),
    },
  ],
  credential: { cosy_key: '', encrypt_user_info: '', user_id: 'u-cn', machine_id: 'm-cn', loaded_at: new Date().toISOString() },
  status: { loaded: true, has_credentials: true, source: 'project_bootstrap', loaded_at: new Date().toISOString() },
  token_stats: { today: 0, week: 0, total: 0 },
  stored_meta: { schema_version: 2, source: 'project_bootstrap', lingma_version_hint: '', obtained_at: '', updated_at: '', token_expire_time: 0 },
  oauth: { has_access_token: true, has_refresh_token: true },
};

const BOOTSTRAP_RESPONSE = {
  id: 'sess-test',
  status: 'awaiting_callback_url',
  method: 'remote_callback',
  region: 'china',
  auth_url: 'https://account.alibabacloud.com/login/login.htm?fake=1',
  started_at: new Date().toISOString(),
  expires_at: new Date(Date.now() + 5 * 60 * 1000).toISOString(),
};

const STATUS_ERROR = {
  ...BOOTSTRAP_RESPONSE,
  status: 'error',
  error: 'timeout: user did not complete login within 5m',
};

const STATUS_COMPLETED = {
  ...BOOTSTRAP_RESPONSE,
  status: 'completed',
  phase: 'saving',
};

const STATUS_WAITING_CACHE = {
  ...BOOTSTRAP_RESPONSE,
  status: 'running',
  phase: 'waiting_lingma_cache',
};

async function mockAccount(page: Page) {
  await page.route('**/admin/account', route => {
    if (route.request().method() === 'GET') {
      return route.fulfill({ json: ACCOUNT_RESPONSE });
    }
    return route.continue();
  });
}

async function gotoAccount(page: Page) {
  await page.goto('/');
  await page.getByRole('link', { name: /账号管理/ }).click();
}

test.describe('Account bootstrap remote_callback flow', () => {
  test('shows account pool and separate region login buttons', async ({ page }) => {
    await mockAccount(page);

    await gotoAccount(page);

    await expect(page.getByText('混合使用 · 账号平均')).toBeVisible();
    await expect(page.getByText('CN Main')).toBeVisible();
    await expect(page.getByText('Global Main')).toBeVisible();
    await expect(page.getByRole('button', { name: /登录国内版/ })).toBeVisible();
    await expect(page.getByRole('button', { name: /登录国际版/ })).toBeVisible();
  });

  test('china login submits remote_callback with china region', async ({ page }) => {
    let submitted: { method?: string; region?: string } | undefined;
    await mockAccount(page);
    await page.route('**/admin/account/bootstrap', route => {
      if (route.request().method() === 'POST') {
        submitted = JSON.parse(route.request().postData() || '{}') as { method?: string; region?: string };
        return route.fulfill({ json: BOOTSTRAP_RESPONSE });
      }
      return route.continue();
    });
    await page.route('**/admin/account/bootstrap/status*', route => route.fulfill({ json: BOOTSTRAP_RESPONSE }));

    await gotoAccount(page);
    await page.getByRole('button', { name: /登录国内版/ }).click();

    await expect.poll(() => submitted?.method).toBe('remote_callback');
    await expect.poll(() => submitted?.region).toBe('china');
    await expect(page.getByText(BOOTSTRAP_RESPONSE.auth_url)).toBeVisible();
    await expect(page.getByPlaceholder(/127.0.0.1:37510/)).toBeVisible();
    await expect(page.getByRole('button', { name: /取消/ })).toBeVisible();
  });

  test('international login submits international region and shows configured error', async ({ page }) => {
    let submittedRegion: string | undefined;
    await mockAccount(page);
    await page.route('**/admin/account/bootstrap', route => {
      submittedRegion = (JSON.parse(route.request().postData() || '{}') as { region?: string }).region;
      return route.fulfill({
        status: 400,
        json: { error: { message: 'international adapter protocol not configured' } },
      });
    });

    await gotoAccount(page);
    await page.getByRole('button', { name: /登录国际版/ }).click();

    await expect.poll(() => submittedRegion).toBe('international');
    await expect(page.getByText(/国际版登录入口已区分/)).toBeVisible();
  });

  test('submit callback button calls submit endpoint', async ({ page }) => {
    let submittedPayload: { id?: string; callback_url?: string } | undefined;
    await mockAccount(page);
    await page.route('**/admin/account/bootstrap', route => {
      if (route.request().method() === 'POST') {
        return route.fulfill({ json: BOOTSTRAP_RESPONSE });
      }
      if (route.request().method() === 'DELETE') {
        return route.fulfill({ json: { status: 'cancelled' } });
      }
      return route.continue();
    });
    await page.route('**/admin/account/bootstrap/submit', route => {
      submittedPayload = JSON.parse(route.request().postData() || '{}') as { id?: string; callback_url?: string };
      return route.fulfill({ json: STATUS_COMPLETED });
    });
    await page.route('**/admin/account/bootstrap/status*', route => route.fulfill({ json: BOOTSTRAP_RESPONSE }));

    await gotoAccount(page);
    await page.getByRole('button', { name: /登录国内版/ }).click();
    await page.getByPlaceholder(/127.0.0.1:37510/).fill('http://127.0.0.1:37510/auth/callback?auth=a&token=b');
    await page.getByRole('button', { name: /提交回调链接/ }).click();

    await expect.poll(() => submittedPayload?.id).toBe('sess-test');
    await expect.poll(() => submittedPayload?.callback_url).toContain('127.0.0.1:37510');
  });

  test('keeps auth_url visible while waiting for Lingma cache import', async ({ page }) => {
    await mockAccount(page);
    await page.route('**/admin/account/bootstrap', route => {
      if (route.request().method() === 'POST') {
        return route.fulfill({ json: BOOTSTRAP_RESPONSE });
      }
      return route.continue();
    });
    await page.route('**/admin/account/bootstrap/status*', route => route.fulfill({ json: STATUS_WAITING_CACHE }));

    await gotoAccount(page);
    await page.getByRole('button', { name: /登录国内版/ }).click();

    await expect(page.getByText(BOOTSTRAP_RESPONSE.auth_url)).toBeVisible();
  });

  test('cancel button calls DELETE endpoint', async ({ page }) => {
    let deleteCalled = false;
    await mockAccount(page);
    await page.route('**/admin/account/bootstrap', route => {
      if (route.request().method() === 'POST') {
        return route.fulfill({ json: BOOTSTRAP_RESPONSE });
      }
      if (route.request().method() === 'DELETE') {
        deleteCalled = true;
        return route.fulfill({ json: { status: 'cancelled' } });
      }
      return route.continue();
    });
    await page.route('**/admin/account/bootstrap/status*', route => route.fulfill({ json: BOOTSTRAP_RESPONSE }));

    await gotoAccount(page);
    await page.getByRole('button', { name: /登录国内版/ }).click();
    await page.getByRole('button', { name: /取消/ }).click();

    await expect.poll(() => deleteCalled).toBe(true);
  });

  test('shows error message on timeout', async ({ page }) => {
    await mockAccount(page);
    await page.route('**/admin/account/bootstrap', route => route.fulfill({ json: BOOTSTRAP_RESPONSE }));
    await page.route('**/admin/account/bootstrap/status*', route => route.fulfill({ json: STATUS_ERROR }));

    await gotoAccount(page);
    await page.getByRole('button', { name: /登录国内版/ }).click();

    await expect(page.getByText(/timeout/i)).toBeVisible();
  });

  test('account test can target a specific account id', async ({ page }) => {
    let testedURL = '';
    await mockAccount(page);
    await page.route('**/admin/account/test*', route => {
      testedURL = route.request().url();
      return route.fulfill({
        json: {
          account_id: 'intl-1',
          account_label: 'Global Main',
          region: 'international',
          success: false,
          status_code: 0,
          response_preview: '',
          error: 'international adapter protocol not configured',
          credential_snapshot: {
            has_cosy_key: false,
            has_encrypt_user_info: false,
            has_user_id: true,
            has_machine_id: false,
            cosy_key_prefix: '',
            user_id: 'u-intl',
          },
          timestamp: new Date().toISOString(),
        },
      });
    });

    await gotoAccount(page);
    await page.locator('tr', { hasText: 'Global Main' }).getByRole('button', { name: '测试' }).click();

    await expect.poll(() => testedURL).toContain('id=intl-1');
    await expect(page.getByText('international adapter protocol not configured')).toBeVisible();
  });
});
