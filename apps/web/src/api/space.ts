import client from './client';
import type { AxiosError } from 'axios';

// 使用构建时环境变量决定是否启用 mock 模式
const useMock = import.meta.env.VITE_USE_MOCK === 'true';

function getApiErrorMessage(err: unknown, fallback: string): string {
  const error = err as AxiosError<{ message?: string }>;
  return error.response?.data?.message || (err instanceof Error ? err.message : fallback);
}

function mockDelay(ms: number): Promise<void> {
  return new Promise((r) => setTimeout(r, ms));
}

/** Space 摘要（FR2-010 / FR2-051） */
export interface SpaceSummary {
  id: string;
  name: string;
  owner_user_id: number;
  role?: string;
  default_max_rating?: string;
  created_at?: string;
}

/** Space 成员（FR2-010 / FR2-051） */
export interface SpaceMember {
  space_id: string;
  user_id: number;
  role: string;
  max_rating?: string | null;
  created_at?: string;
  updated_at?: string;
}

/** 系统用户（FR2-010，默认 Space owner 可管） */
export interface ManagedUser {
  id: number;
  username: string;
  status: string;
  created_at?: string;
}

// ─── 真实 API ────────────────────────────────────────

async function realListSpaces(): Promise<SpaceSummary[]> {
  const res = await client.get<{ items: SpaceSummary[] }>('/api/spaces');
  return res.data.items ?? [];
}

async function realListSpaceMembers(spaceID: string): Promise<SpaceMember[]> {
  const res = await client.get<{ items: SpaceMember[] }>(`/api/spaces/${spaceID}/members`);
  return res.data.items ?? [];
}

async function realCreateSpace(id: string, name: string): Promise<SpaceSummary> {
  try {
    const res = await client.post<SpaceSummary>('/api/spaces', { id, name });
    return res.data;
  } catch (err) {
    throw new Error(getApiErrorMessage(err, '创建 Space 失败'), { cause: err });
  }
}

/** 添加/更新成员角色（owner）；role 仅 editor|viewer */
async function realAddSpaceMember(
  spaceID: string,
  input: { user_id?: number; username?: string; role: string },
): Promise<void> {
  try {
    await client.post(`/api/spaces/${spaceID}/members`, input);
  } catch (err) {
    throw new Error(getApiErrorMessage(err, '添加成员失败'), { cause: err });
  }
}

async function realRemoveSpaceMember(spaceID: string, userID: number): Promise<void> {
  try {
    await client.delete(`/api/spaces/${spaceID}/members/${userID}`);
  } catch (err) {
    throw new Error(getApiErrorMessage(err, '移除成员失败'), { cause: err });
  }
}

async function realListUsers(): Promise<ManagedUser[]> {
  try {
    const res = await client.get<{ items: ManagedUser[] }>('/api/users');
    return res.data.items ?? [];
  } catch (err) {
    // 保留 HTTP status 供 UI 识别 403（非默认 Space owner）
    const ax = err as AxiosError<{ message?: string }>;
    const status = ax.response?.status;
    const e = new Error(getApiErrorMessage(err, '加载用户列表失败'), { cause: err }) as Error & {
      status?: number;
    };
    if (status) e.status = status;
    throw e;
  }
}

async function realCreateUser(username: string, password: string): Promise<ManagedUser> {
  try {
    const res = await client.post<ManagedUser>('/api/users', { username, password });
    return res.data;
  } catch (err) {
    throw new Error(getApiErrorMessage(err, '创建用户失败'), { cause: err });
  }
}

async function realSetUserStatus(userID: number, status: string): Promise<void> {
  try {
    await client.put(`/api/users/${userID}/status`, { status });
  } catch (err) {
    throw new Error(getApiErrorMessage(err, '更新用户状态失败'), { cause: err });
  }
}

/** 设置 Space 默认最高可见分级（FR2-051，owner，需密码确认） */
async function realUpdateSpaceParental(
  spaceID: string,
  password: string,
  defaultMaxRating: string,
): Promise<void> {
  try {
    await client.put(`/api/spaces/${spaceID}/parental`, {
      password,
      default_max_rating: defaultMaxRating,
    });
  } catch (err) {
    throw new Error(getApiErrorMessage(err, '更新家长控制策略失败'), { cause: err });
  }
}

/** 设置成员最高可见分级（FR2-051，owner，需密码确认）；空串清除覆盖 */
async function realUpdateMemberMaxRating(
  spaceID: string,
  userID: number,
  password: string,
  maxRating: string,
): Promise<void> {
  try {
    await client.put(`/api/spaces/${spaceID}/members/${userID}/max-rating`, {
      password,
      max_rating: maxRating,
    });
  } catch (err) {
    throw new Error(getApiErrorMessage(err, '更新成员可见分级失败'), { cause: err });
  }
}

// ─── Mock ────────────────────────────────────────────

const mockSpaces: SpaceSummary[] = [
  {
    id: 'space-default',
    name: '默认',
    owner_user_id: 1,
    role: 'owner',
    default_max_rating: '',
  },
];

const mockMembers: SpaceMember[] = [
  {
    space_id: 'space-default',
    user_id: 1,
    role: 'owner',
    max_rating: null,
  },
];

const mockUsers: ManagedUser[] = [
  { id: 1, username: 'admin', status: 'active', created_at: '2026-01-01T00:00:00Z' },
];

async function mockListSpaces(): Promise<SpaceSummary[]> {
  await mockDelay(80);
  return [...mockSpaces];
}

async function mockListSpaceMembers(spaceID: string): Promise<SpaceMember[]> {
  await mockDelay(80);
  return mockMembers.filter((m) => m.space_id === spaceID);
}

async function mockCreateSpace(id: string, name: string): Promise<SpaceSummary> {
  await mockDelay(120);
  const tid = id.trim();
  const tname = name.trim();
  if (!tid || !tname) throw new Error('id 与 name 必填');
  if (mockSpaces.some((s) => s.id === tid)) throw new Error('Space 已存在');
  const sp: SpaceSummary = {
    id: tid,
    name: tname,
    owner_user_id: 1,
    role: 'owner',
    default_max_rating: '',
  };
  mockSpaces.push(sp);
  mockMembers.push({ space_id: tid, user_id: 1, role: 'owner', max_rating: null });
  return sp;
}

async function mockAddSpaceMember(
  spaceID: string,
  input: { user_id?: number; username?: string; role: string },
): Promise<void> {
  await mockDelay(100);
  const role = (input.role || '').trim();
  if (role !== 'editor' && role !== 'viewer') throw new Error('角色无效');
  let uid = input.user_id ?? 0;
  if (uid <= 0 && input.username) {
    const u = mockUsers.find((x) => x.username === input.username?.trim());
    if (!u) throw new Error('用户不存在');
    uid = u.id;
  }
  if (uid <= 0) throw new Error('需要 user_id 或 username');
  const existing = mockMembers.find((m) => m.space_id === spaceID && m.user_id === uid);
  if (existing) {
    existing.role = role;
    return;
  }
  mockMembers.push({ space_id: spaceID, user_id: uid, role, max_rating: null });
}

async function mockRemoveSpaceMember(spaceID: string, userID: number): Promise<void> {
  await mockDelay(100);
  const m = mockMembers.find((x) => x.space_id === spaceID && x.user_id === userID);
  if (!m) throw new Error('成员不存在');
  if (m.role === 'owner') throw new Error('不能移除 Space owner');
  const idx = mockMembers.indexOf(m);
  mockMembers.splice(idx, 1);
}

async function mockListUsers(): Promise<ManagedUser[]> {
  await mockDelay(80);
  return [...mockUsers];
}

async function mockCreateUser(username: string, password: string): Promise<ManagedUser> {
  await mockDelay(120);
  const name = username.trim();
  if (!name || password.length < 6) throw new Error('用户名或密码无效');
  if (mockUsers.some((u) => u.username === name)) throw new Error('用户名已存在');
  const u: ManagedUser = {
    id: Math.max(0, ...mockUsers.map((x) => x.id)) + 1,
    username: name,
    status: 'active',
    created_at: new Date().toISOString(),
  };
  mockUsers.push(u);
  return u;
}

async function mockSetUserStatus(userID: number, status: string): Promise<void> {
  await mockDelay(100);
  if (status !== 'active' && status !== 'disabled') throw new Error('状态无效');
  if (userID === 1 && status === 'disabled') throw new Error('不能禁用当前登录用户');
  const u = mockUsers.find((x) => x.id === userID);
  if (!u) throw new Error('用户不存在');
  u.status = status;
}

async function mockUpdateSpaceParental(
  spaceID: string,
  password: string,
  defaultMaxRating: string,
): Promise<void> {
  await mockDelay(120);
  if (password !== 'admin') throw new Error('密码确认失败');
  const sp = mockSpaces.find((s) => s.id === spaceID);
  if (sp) sp.default_max_rating = defaultMaxRating;
}

async function mockUpdateMemberMaxRating(
  spaceID: string,
  userID: number,
  password: string,
  maxRating: string,
): Promise<void> {
  await mockDelay(120);
  if (password !== 'admin') throw new Error('密码确认失败');
  const m = mockMembers.find((x) => x.space_id === spaceID && x.user_id === userID);
  if (!m) throw new Error('成员不存在');
  m.max_rating = maxRating.trim() === '' ? null : maxRating;
}

// ─── 导出 ────────────────────────────────────────────

export function listSpaces() {
  return useMock ? mockListSpaces() : realListSpaces();
}
export function listSpaceMembers(spaceID: string) {
  return useMock ? mockListSpaceMembers(spaceID) : realListSpaceMembers(spaceID);
}
export function createSpace(id: string, name: string) {
  return useMock ? mockCreateSpace(id, name) : realCreateSpace(id, name);
}
export function addSpaceMember(
  spaceID: string,
  input: { user_id?: number; username?: string; role: string },
) {
  return useMock ? mockAddSpaceMember(spaceID, input) : realAddSpaceMember(spaceID, input);
}
export function removeSpaceMember(spaceID: string, userID: number) {
  return useMock ? mockRemoveSpaceMember(spaceID, userID) : realRemoveSpaceMember(spaceID, userID);
}
export function listUsers() {
  return useMock ? mockListUsers() : realListUsers();
}
export function createUser(username: string, password: string) {
  return useMock ? mockCreateUser(username, password) : realCreateUser(username, password);
}
export function setUserStatus(userID: number, status: string) {
  return useMock ? mockSetUserStatus(userID, status) : realSetUserStatus(userID, status);
}
export function updateSpaceParental(spaceID: string, password: string, defaultMaxRating: string) {
  return useMock
    ? mockUpdateSpaceParental(spaceID, password, defaultMaxRating)
    : realUpdateSpaceParental(spaceID, password, defaultMaxRating);
}
export function updateMemberMaxRating(
  spaceID: string,
  userID: number,
  password: string,
  maxRating: string,
) {
  return useMock
    ? mockUpdateMemberMaxRating(spaceID, userID, password, maxRating)
    : realUpdateMemberMaxRating(spaceID, userID, password, maxRating);
}
