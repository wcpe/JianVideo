import { useEffect, useState } from 'react';
import { useParams } from 'react-router-dom';
import {
  Container,
  Title,
  Text,
  Loader,
  Center,
  SimpleGrid,
  Card,
  Image,
  Button,
  Group,
  Stack,
  Modal,
  PasswordInput,
} from '@mantine/core';
import { IconDownload, IconLock } from '@tabler/icons-react';
import {
  getShareInfo,
  shareRawURL,
  shareThumbnailURL,
  shareDownloadURL,
  shareStreamURL,
} from '@/api/share';
import { isImageFile, mediaDisplayName } from '@/utils/media';
import type { ShareInfo, MediaFile } from '@/types';

/** 单个被分享媒体的查看：图片直出、视频渐进式在线播放，附原文件下载（FR-43） */
function SharedMedia({ token, media }: { token: string; media: MediaFile }) {
  const isImage = isImageFile(media);
  return (
    <Stack>
      <Title order={4}>{mediaDisplayName(media)}</Title>
      {isImage ? (
        <Image src={shareRawURL(token, media.id)} alt={media.file_name} fit="contain" mah="70vh" />
      ) : (
        // 公开播放走渐进式 stream（非转码/HLS），原生 video 适配常见格式
        <video
          src={shareStreamURL(token, media.id)}
          controls
          style={{ width: '100%', maxHeight: '70vh', background: '#000' }}
        />
      )}
      <Group justify="center">
        <Button
          component="a"
          href={shareDownloadURL(token, media.id)}
          download
          variant="light"
          leftSection={<IconDownload size={16} />}
        >
          下载原文件
        </Button>
      </Group>
    </Stack>
  );
}

/** 分享需密码时的输入门（FR-78）：提交密码后由父组件带头重拉元信息 */
function PasswordGate({
  onSubmit,
  submitting,
  wrong,
}: {
  onSubmit: (pwd: string) => void;
  submitting: boolean;
  wrong: boolean;
}) {
  const [pwd, setPwd] = useState('');
  return (
    <Center h="100vh">
      <Stack
        w={320}
        component="form"
        onSubmit={(e) => {
          e.preventDefault();
          onSubmit(pwd);
        }}
      >
        <Group gap="xs" justify="center">
          <IconLock size={20} />
          <Title order={4}>该分享需要密码</Title>
        </Group>
        <PasswordInput
          value={pwd}
          onChange={(e) => setPwd(e.currentTarget.value)}
          placeholder="请输入访问密码"
          aria-label="访问密码"
          autoFocus
          error={wrong ? '分享不存在或已过期' : undefined}
        />
        <Button type="submit" loading={submitting}>
          访问
        </Button>
      </Stack>
    </Center>
  );
}

/** 公开分享查看页（FR-43，免登；密码门禁见 FR-78）：按类型展示图片 / 视频 / 相册网格 */
export default function SharePage() {
  const { token = '' } = useParams();
  const [info, setInfo] = useState<ShareInfo | null>(null);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);
  const [selected, setSelected] = useState<MediaFile | null>(null);
  // 密码门禁状态（FR-78）
  const [needPassword, setNeedPassword] = useState(false);
  const [wrongPassword, setWrongPassword] = useState(false);
  const [submitting, setSubmitting] = useState(false);

  // 初次加载：不带密码拉取，若返回 requires_password 则进入密码门
  useEffect(() => {
    let active = true;
    setLoading(true);
    getShareInfo(token)
      .then((d) => {
        if (!active) return;
        if (d.requires_password && !d.media && !d.album && !(d.items && d.items.length)) {
          setNeedPassword(true);
        } else {
          setInfo(d);
        }
      })
      .catch(() => {
        if (active) setError('分享不存在或已过期');
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [token]);

  // 提交密码：带 X-Share-Password 头重拉，仍要密码即视为密码错
  function submitPassword(pwd: string) {
    setSubmitting(true);
    setWrongPassword(false);
    getShareInfo(token, pwd)
      .then((d) => {
        if (d.requires_password && !d.media && !d.album && !(d.items && d.items.length)) {
          setWrongPassword(true);
        } else {
          setInfo(d);
          setNeedPassword(false);
        }
      })
      .catch(() => setError('分享不存在或已过期'))
      .finally(() => setSubmitting(false));
  }

  if (loading)
    return (
      <Center h="100vh">
        <Loader />
      </Center>
    );
  if (error)
    return (
      <Center h="100vh">
        <Text c="dimmed">{error}</Text>
      </Center>
    );
  if (needPassword)
    return <PasswordGate onSubmit={submitPassword} submitting={submitting} wrong={wrongPassword} />;
  if (!info)
    return (
      <Center h="100vh">
        <Text c="dimmed">分享不存在或已过期</Text>
      </Center>
    );

  return (
    <Container py="xl" size="lg">
      {info.resource_type === 'media' && info.media && (
        <SharedMedia token={token} media={info.media} />
      )}

      {info.resource_type === 'album' && (
        <Stack>
          <Title order={3}>{info.album?.name ?? '相册分享'}</Title>
          {(info.items ?? []).length === 0 ? (
            <Text c="dimmed">该相册暂无可访问的内容</Text>
          ) : (
            <SimpleGrid cols={{ base: 2, sm: 3, md: 4 }}>
              {(info.items ?? []).map((m) => (
                <Card
                  key={m.id}
                  padding="xs"
                  withBorder
                  style={{ cursor: 'pointer' }}
                  onClick={() => setSelected(m)}
                >
                  <Card.Section>
                    <Image
                      src={shareThumbnailURL(token, m.id)}
                      alt={m.file_name}
                      h={140}
                      fit="cover"
                    />
                  </Card.Section>
                  <Text size="xs" mt="xs" truncate>
                    {mediaDisplayName(m)}
                  </Text>
                </Card>
              ))}
            </SimpleGrid>
          )}
          <Modal
            opened={!!selected}
            onClose={() => setSelected(null)}
            size="xl"
            centered
            title={selected ? mediaDisplayName(selected) : ''}
          >
            {selected && <SharedMedia token={token} media={selected} />}
          </Modal>
        </Stack>
      )}
    </Container>
  );
}
