import { useState, useEffect, useRef } from 'react';
import {
  AlertTriangle,
  CheckCircle,
  ClipboardPaste,
  Cloud,
  Globe2,
  Radio,
  RefreshCw,
  Server,
  ShieldCheck,
  X,
  Zap,
} from 'lucide-react';
import {
  cancelBootstrap,
  getAccount,
  getBootstrapStatus,
  refreshAccount,
  startBootstrap,
  submitBootstrapCallback,
  testAccountConnection,
} from '../api/client';
import { StatCard } from '../components/StatCard';
import { Skeleton } from '../components/Skeleton';
import type {
  AccountData,
  AccountRegion,
  AccountSummary,
  AccountTestResult,
  BootstrapMethod,
  BootstrapResponse,
} from '../types';

const BOOTSTRAP_PHASES = [
  { key: 'awaiting_callback_url', label: '打开登录', icon: '1' },
  { key: 'waiting_lingma_cache', label: '等待登录', icon: '2' },
  { key: 'parsing_callback', label: '解析回调', icon: '3' },
  { key: 'importing_lingma_cache', label: '导入凭据', icon: '4' },
  { key: 'generating_cosy', label: '生成 COSY', icon: '5' },
  { key: 'deriving_remote', label: '远程派生', icon: '6' },
  { key: 'saving', label: '保存完成', icon: '7' },
];

const REGION_LABEL: Record<AccountRegion, string> = {
  china: '国内版',
  international: '国际版',
};

const ROUTING_LABEL: Record<string, string> = {
  china_only: '只使用国内版',
  international_only: '只使用国际版',
  mixed: '混合使用',
};

const LOAD_BALANCE_LABEL: Record<string, string> = {
  round_robin: '账号平均',
};

function PhaseProgress({ phase }: { phase?: string }) {
  if (!phase) return null;
  const currentIdx = BOOTSTRAP_PHASES.findIndex(p => p.key === phase);
  return (
    <div className="account-phase-grid">
      {BOOTSTRAP_PHASES.map((p, i) => {
        const active = currentIdx >= 0 && i <= currentIdx;
        const isCurrent = i === currentIdx;
        return (
          <div key={p.key} className={`account-phase ${active ? 'active' : ''} ${isCurrent ? 'current' : ''}`}>
            <span>{p.icon}</span>
            {p.label}
          </div>
        );
      })}
    </div>
  );
}

function Badge({ ok, label }: { ok: boolean; label: string }) {
  return (
    <span className={`badge ${ok ? 'badge-success' : 'badge-error'}`} style={{ fontSize: 12 }}>
      {ok ? label : `缺少 ${label}`}
    </span>
  );
}

function RegionBadge({ region }: { region?: AccountRegion | string }) {
  const normalized = region === 'international' ? 'international' : 'china';
  return (
    <span className={`account-region-badge account-region-${normalized}`}>
      {normalized === 'international' ? <Globe2 size={13} /> : <Server size={13} />}
      {REGION_LABEL[normalized]}
    </span>
  );
}

function formatExpireTime(value: string | number | undefined): string {
  if (value === undefined || value === '' || value === 0) return '-';
  const n = typeof value === 'number' ? value : parseInt(value, 10);
  if (!Number.isFinite(n) || n <= 0) return '-';
  return new Date(n).toLocaleString();
}

function isExpired(value: string | number | undefined): boolean {
  if (value === undefined || value === '' || value === 0) return false;
  const n = typeof value === 'number' ? value : parseInt(value, 10);
  return Number.isFinite(n) && n > 0 && Date.now() > n;
}

function formatTime(value?: string): string {
  if (!value) return '-';
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
}

function accountName(account: AccountSummary): string {
  return account.label || account.user_id || account.id || `${REGION_LABEL[account.region]}账号`;
}

export function Account() {
  const [data, setData] = useState<AccountData | null>(null);
  const [refreshing, setRefreshing] = useState(false);
  const [bootstrap, setBootstrap] = useState<BootstrapResponse | null>(null);
  const [callbackURL, setCallbackURL] = useState('');
  const [submittingCallback, setSubmittingCallback] = useState(false);
  const [bootstrapRegion, setBootstrapRegion] = useState<AccountRegion>('china');
  const [testResult, setTestResult] = useState<AccountTestResult | null>(null);
  const [testingAccountId, setTestingAccountId] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [remaining, setRemaining] = useState<string>('');
  const pollRef = useRef<ReturnType<typeof setInterval>>();
  const tickRef = useRef<ReturnType<typeof setInterval>>();

  const load = async () => {
    setLoading(true);
    try {
      setData(await getAccount());
    } catch {
      // Keep existing page state; admin shell handles auth failures globally.
    }
    setLoading(false);
  };

  useEffect(() => { load(); }, []);

  useEffect(() => {
    return () => {
      if (pollRef.current) clearInterval(pollRef.current);
      if (tickRef.current) clearInterval(tickRef.current);
    };
  }, []);

  const formatRemaining = (expiresAt?: string): string => {
    if (!expiresAt) return '';
    const ms = new Date(expiresAt).getTime() - Date.now();
    if (ms <= 0) return '已超时';
    const total = Math.floor(ms / 1000);
    const m = Math.floor(total / 60);
    const s = total % 60;
    return `${m}:${s.toString().padStart(2, '0')}`;
  };

  useEffect(() => {
    const status = bootstrap?.status;
    const inFlight = status === 'awaiting_callback_url' || status === 'running';
    if (inFlight) {
      setRemaining(formatRemaining(bootstrap?.expires_at));
      tickRef.current = setInterval(() => {
        setRemaining(formatRemaining(bootstrap?.expires_at));
      }, 1000);
      return () => {
        if (tickRef.current) clearInterval(tickRef.current);
      };
    }
    if (tickRef.current) {
      clearInterval(tickRef.current);
      tickRef.current = undefined;
    }
    setRemaining('');
    return undefined;
  }, [bootstrap?.status, bootstrap?.expires_at]);

  const handleRefresh = async () => {
    setRefreshing(true);
    try {
      await refreshAccount();
      await load();
    } catch {
      // The follow-up load will show the last known state.
    }
    setRefreshing(false);
  };

  const handleTest = async (accountId?: string) => {
    setTestingAccountId(accountId || '__default__');
    setTestResult(null);
    try {
      setTestResult(await testAccountConnection(accountId));
    } catch (e) {
      setTestResult({
        account_id: accountId || '',
        success: false,
        status_code: 0,
        response_preview: '',
        error: e instanceof Error ? e.message : String(e),
        credential_snapshot: {
          has_cosy_key: false,
          has_encrypt_user_info: false,
          has_user_id: false,
          has_machine_id: false,
          cosy_key_prefix: '',
          user_id: '',
        },
        timestamp: new Date().toISOString(),
      });
    }
    setTestingAccountId(null);
  };

  const startPolling = (id: string) => {
    if (pollRef.current) clearInterval(pollRef.current);
    pollRef.current = setInterval(async () => {
      try {
        const status = await getBootstrapStatus(id);
        setBootstrap(status);
        if (status.status === 'completed') {
          clearInterval(pollRef.current);
          await load();
        } else if (status.status === 'error' || status.status === 'cancelled') {
          clearInterval(pollRef.current);
        }
      } catch {
        // Keep polling transient backend failures.
      }
    }, 2000);
  };

  const handleBootstrap = async (region: AccountRegion, method: BootstrapMethod = 'remote_callback') => {
    setCallbackURL('');
    setBootstrapRegion(region);
    try {
      const resp = await startBootstrap(method, region);
      setBootstrap(resp);
      startPolling(resp.id);
    } catch (e) {
      setBootstrap({
        id: '',
        status: 'error',
        method,
        region,
        error: e instanceof Error ? e.message : String(e),
        started_at: '',
      });
    }
  };

  const handleSubmitCallback = async () => {
    if (!bootstrap?.id || !callbackURL.trim()) return;
    setSubmittingCallback(true);
    try {
      const resp = await submitBootstrapCallback({ id: bootstrap.id, callback_url: callbackURL.trim() });
      setBootstrap(resp);
      if (resp.status === 'completed') {
        await load();
      } else {
        startPolling(resp.id);
      }
    } catch (e) {
      setBootstrap(prev => prev ? {
        ...prev,
        status: 'error',
        error: e instanceof Error ? e.message : String(e),
      } : {
        id: '',
        status: 'error',
        method: 'remote_callback',
        region: bootstrapRegion,
        error: e instanceof Error ? e.message : String(e),
        started_at: '',
      });
    }
    setSubmittingCallback(false);
  };

  const handleCancel = async () => {
    if (!bootstrap?.id) return;
    try {
      await cancelBootstrap(bootstrap.id);
      if (pollRef.current) clearInterval(pollRef.current);
      setBootstrap({ ...bootstrap, status: 'cancelled', error: '', phase: '' });
    } catch (e) {
      setBootstrap({
        ...bootstrap,
        status: 'error',
        error: e instanceof Error ? e.message : String(e),
      });
    }
  };

  const fmtToken = (n: number) => n >= 1000000 ? `${(n / 1000000).toFixed(1)}M` : n >= 1000 ? `${(n / 1000).toFixed(1)}K` : String(n);
  const inFlight = bootstrap?.status === 'awaiting_callback_url' || bootstrap?.status === 'running';
  const waitingLingmaCache = bootstrap?.phase === 'waiting_lingma_cache' || bootstrap?.phase === 'importing_lingma_cache';
  const showBootstrapAuthURL = Boolean(bootstrap?.auth_url && inFlight);
  const showManualCallbackInput = bootstrap?.status === 'awaiting_callback_url' && !waitingLingmaCache;

  if (loading || !data) {
    return (
      <div>
        <div className="page-header"><h2>账号管理</h2></div>
        <Skeleton variant="rect" style={{ height: 120, marginBottom: 16 }} />
        <Skeleton variant="rect" style={{ height: 180, marginBottom: 16 }} />
        <Skeleton variant="rect" style={{ height: 220 }} />
      </div>
    );
  }

  const accounts = data.accounts || [];
  const counts = data.counts || {
    total: accounts.length,
    enabled: accounts.filter(item => item.enabled).length,
    china: accounts.filter(item => item.region === 'china').length,
    international: accounts.filter(item => item.region === 'international').length,
  };
  const routingMode = data.routing_mode || 'mixed';
  const loadBalance = data.load_balance || 'round_robin';
  const hasCosy = data.credential?.cosy_key !== '';
  const hasEUI = data.credential?.encrypt_user_info !== '';
  const tokenExpired = isExpired(data.stored_meta?.token_expire_time || '');

  return (
    <div>
      <div className="page-header">
        <div>
          <h2>账号管理</h2>
          <div className="page-meta-inline">
            {ROUTING_LABEL[routingMode] || routingMode} · {LOAD_BALANCE_LABEL[loadBalance] || loadBalance}
          </div>
        </div>
        <div className="page-actions page-actions-wrap">
          <button
            className="btn btn-primary"
            onClick={() => handleBootstrap('china')}
            disabled={inFlight}
            title="使用国内版 Lingma 登录并保存为中国区账号"
          >
            <Server size={16} />
            登录国内版
          </button>
          <button
            className="btn"
            onClick={() => handleBootstrap('international')}
            disabled={inFlight}
            title="使用国际版 Lingma 登录。当前后端会在协议未配置时返回明确错误。"
          >
            <Globe2 size={16} />
            登录国际版
          </button>
          <button className="btn" onClick={handleRefresh} disabled={refreshing}>
            <RefreshCw size={16} />
            {refreshing ? '读取中...' : '重新读取'}
          </button>
        </div>
      </div>

      <div className="stat-grid account-stat-grid">
        <StatCard label="账号总数" value={counts.total || 0} icon={ShieldCheck} />
        <StatCard label="启用账号" value={counts.enabled || 0} icon={Radio} />
        <StatCard label="国内版" value={counts.china || 0} icon={Server} />
        <StatCard label="国际版" value={counts.international || 0} icon={Globe2} />
      </div>

      {bootstrap && (
        <div className="card account-bootstrap-card">
          <div className="account-card-header">
            <div>
              <div className="account-card-kicker"><RegionBadge region={bootstrap.region || bootstrapRegion} /></div>
              <h4>
                {bootstrap.status === 'awaiting_callback_url' && '等待回填回调链接'}
                {bootstrap.status === 'running' && (waitingLingmaCache ? '等待浏览器登录完成' : '正在处理回调链接')}
                {bootstrap.status === 'completed' && '登录完成'}
                {bootstrap.status === 'error' && '登录失败'}
                {bootstrap.status === 'cancelled' && '已取消'}
              </h4>
            </div>
            {inFlight && (
              <div className="account-card-actions">
                {remaining && <span className="page-meta-inline">剩余 {remaining}</span>}
                <button className="btn" onClick={handleCancel}>
                  <X size={14} /> 取消
                </button>
              </div>
            )}
          </div>

          {showBootstrapAuthURL && (
            <div className="account-login-link">
              <p>在浏览器中打开登录链接，完成登录后将最终的 127.0.0.1:37510 回调地址粘贴回来。</p>
              <a href={bootstrap.auth_url} target="_blank" rel="noopener noreferrer">
                {bootstrap.auth_url}
              </a>
            </div>
          )}

          {showManualCallbackInput && (
            <div className="account-callback-box">
              <textarea
                value={callbackURL}
                onChange={(e) => setCallbackURL(e.target.value)}
                placeholder="粘贴完整的 http://127.0.0.1:37510/... 回调链接"
                rows={4}
              />
              <button className="btn btn-primary" onClick={handleSubmitCallback} disabled={submittingCallback || !callbackURL.trim()}>
                <ClipboardPaste size={16} />
                {submittingCallback ? '提交中...' : '提交回调链接'}
              </button>
            </div>
          )}

          {waitingLingmaCache && (
            <p className="account-muted">控制台正在读取 Lingma 本地凭据缓存，完成后会自动刷新账号池。</p>
          )}
          {bootstrap.status === 'running' && !showBootstrapAuthURL && (
            <p className="account-muted">正在解析回调参数并生成凭据，请稍候。</p>
          )}
          <PhaseProgress phase={bootstrap.phase || bootstrap.status} />
          {bootstrap.status === 'completed' && <p className="account-success">凭据已保存到账号池。</p>}
          {bootstrap.status === 'cancelled' && <p className="account-muted">登录流程已取消，未写入凭据。</p>}
          {bootstrap.status === 'error' && (
            <p className="account-error">
              {bootstrap.error || '未知错误'}
              {bootstrap.error?.includes('international adapter protocol not configured') && (
                <span> 国际版登录入口已区分，后端协议实现完成后即可接入真实登录。</span>
              )}
            </p>
          )}
        </div>
      )}

      {testResult && (
        <div className={`card account-test-card ${testResult.success ? 'success' : 'error'}`}>
          <h4>
            {testResult.success ? <CheckCircle size={18} color="var(--success)" /> : <AlertTriangle size={18} color="var(--error)" />}
            API 连接测试
          </h4>
          <table>
            <tbody>
              <tr><td style={{ fontWeight: 600 }}>账号</td><td>{testResult.account_label || testResult.account_id || '默认账号'}</td></tr>
              <tr><td style={{ fontWeight: 600 }}>区域</td><td>{testResult.region ? <RegionBadge region={testResult.region} /> : '-'}</td></tr>
              <tr><td style={{ fontWeight: 600 }}>状态</td><td>
                <span className={`badge ${testResult.success ? 'badge-success' : 'badge-error'}`}>
                  {testResult.success ? '成功' : '失败'}
                </span>
              </td></tr>
              <tr><td style={{ fontWeight: 600 }}>HTTP 状态码</td><td>{testResult.status_code || '-'}</td></tr>
              <tr><td style={{ fontWeight: 600 }}>响应预览</td><td>{testResult.response_preview || '-'}</td></tr>
              {testResult.error && <tr><td style={{ fontWeight: 600 }}>错误信息</td><td style={{ color: 'var(--error)' }}>{testResult.error}</td></tr>}
              <tr><td style={{ fontWeight: 600 }}>UserID</td><td><Badge ok={testResult.credential_snapshot.has_user_id} label="UserID" /> {testResult.credential_snapshot.user_id}</td></tr>
            </tbody>
          </table>
        </div>
      )}

      <div className="card card-table account-table-card">
        <div className="account-table-header">
          <div>
            <h4>账号池</h4>
            <p>{accounts.length > 0 ? '请求会按配置模式筛选账号，再按账号平均策略分配。' : '还没有保存账号。'}</p>
          </div>
          <button className="btn" onClick={() => handleTest()} disabled={Boolean(testingAccountId)}>
            <Zap size={16} />
            {testingAccountId === '__default__' ? '测试中...' : '测试默认账号'}
          </button>
        </div>
        {accounts.length > 0 ? (
          <table>
            <thead>
              <tr>
                <th>账号</th>
                <th>区域</th>
                <th>状态</th>
                <th>凭据</th>
                <th>来源</th>
                <th>更新时间</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {accounts.map(account => (
                <tr key={account.id || `${account.region}-${account.user_id}`}>
                  <td>
                    <div className="account-name">{accountName(account)}</div>
                    <div className="account-id">{account.id || '-'}</div>
                  </td>
                  <td><RegionBadge region={account.region} /></td>
                  <td>
                    <span className={`badge ${account.enabled && !account.token_expired ? 'badge-success' : 'badge-error'}`}>
                      {account.enabled ? (account.token_expired ? 'Token 过期' : '启用') : '停用'}
                    </span>
                  </td>
                  <td>
                    <div className="account-credential-badges">
                      {account.region === 'china' && <Badge ok={account.has_cosy_key} label="CosyKey" />}
                      {account.region === 'china' && <Badge ok={account.has_encrypt_info} label="EncryptInfo" />}
                      {account.region === 'international' && <Badge ok={account.has_access_token} label="Access Token" />}
                      {account.region === 'international' && <Badge ok={account.has_refresh_token} label="Refresh Token" />}
                    </div>
                  </td>
                  <td>{account.source || '-'}</td>
                  <td>{formatTime(account.updated_at || account.obtained_at)}</td>
                  <td>
                    <button className="btn" onClick={() => handleTest(account.id)} disabled={Boolean(testingAccountId) || !account.enabled}>
                      <Zap size={14} />
                      {testingAccountId === account.id ? '测试中...' : '测试'}
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        ) : (
          <div className="empty-state account-empty">
            <Cloud className="empty-state-icon" size={28} />
            <div className="empty-state-title">账号池为空</div>
            <div className="empty-state-desc">先登录国内版账号；国际版入口已经分开，等待后端协议实现后可保存国际版凭据。</div>
          </div>
        )}
      </div>

      <div className="account-detail-grid">
        <div className="card">
          <h4>兼容凭据视图</h4>
          <table>
            <tbody>
              <tr><td style={{ fontWeight: 600 }}>UserID</td><td>{data.credential?.user_id || '-'}</td></tr>
              <tr><td style={{ fontWeight: 600 }}>MachineID</td><td>{data.credential?.machine_id || '-'}</td></tr>
              <tr><td style={{ fontWeight: 600 }}>CosyKey</td><td><Badge ok={hasCosy} label="CosyKey" /></td></tr>
              <tr><td style={{ fontWeight: 600 }}>EncryptUserInfo</td><td><Badge ok={hasEUI} label="EncryptUserInfo" /></td></tr>
            </tbody>
          </table>
        </div>

        <div className="card">
          <h4>凭据状态</h4>
          <table>
            <tbody>
              <tr>
                <td style={{ fontWeight: 600 }}>状态</td>
                <td><span className={`badge ${data.status?.loaded ? 'badge-success' : 'badge-error'}`}>
                  {data.status?.loaded ? '已加载' : '未加载'}
                </span></td>
              </tr>
              {tokenExpired && (
                <tr>
                  <td style={{ fontWeight: 600 }}>Token 过期</td>
                  <td><span className="badge badge-error"><AlertTriangle size={12} style={{ marginRight: 4 }} />已过期</span></td>
                </tr>
              )}
              <tr><td style={{ fontWeight: 600 }}>来源</td><td>{data.status?.source || '-'}</td></tr>
              <tr><td style={{ fontWeight: 600 }}>加载时间</td><td>{formatTime(data.status?.loaded_at)}</td></tr>
              <tr><td style={{ fontWeight: 600 }}>Token 过期时间</td><td>{formatExpireTime(data.stored_meta?.token_expire_time)}</td></tr>
              <tr><td style={{ fontWeight: 600 }}>Lingma 版本</td><td>{data.stored_meta?.lingma_version_hint || '-'}</td></tr>
            </tbody>
          </table>
        </div>
      </div>

      <div className="card">
        <h4>Token 用量统计</h4>
        <div className="stat-grid">
          <StatCard label="今日" value={fmtToken(data.token_stats?.today || 0)} />
          <StatCard label="本周" value={fmtToken(data.token_stats?.week || 0)} />
          <StatCard label="总计" value={fmtToken(data.token_stats?.total || 0)} />
        </div>
      </div>
    </div>
  );
}
