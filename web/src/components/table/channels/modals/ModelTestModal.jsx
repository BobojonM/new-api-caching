/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

import React from 'react';
import {
  Modal,
  Button,
  Input,
  Table,
  Tag,
  Typography,
  Space,
  Tooltip,
} from '@douyinfe/semi-ui';
import { IconSearch } from '@douyinfe/semi-icons';
import { copy, showError, showInfo, showSuccess } from '../../../../helpers/index.js';
import { MODEL_TABLE_PAGE_SIZE } from '../../../../constants/index.js';

const TEST_KINDS = {
  TEXT: 'text',
  FUNCTION: 'function',
  JSON: 'json',
};

const buildKey = (channelId, model, type) => `${channelId}-${model}-${type}`;

const ModelTestModal = ({
  // visibility & context
  showModelTestModal,
  currentTestChannel,
  handleCloseModal,

  // batching
  isBatchTesting,
  batchTestModels,

  // search & selection
  modelSearchKeyword,
  setModelSearchKeyword,
  selectedModelKeys,
  setSelectedModelKeys,

  // results & running states
  modelTestResults, // { "<id>-<model>-<type>": { success, time } }
  testingKeys,      // Set("<id>-<model>-<type>")

  // trigger single test
  testChannel,

  // pagination
  modelTablePage,
  setModelTablePage,

  // misc
  allSelectingRef,
  isMobile,
  t,
}) => {
  const hasChannel = Boolean(currentTestChannel);
  const channelId = hasChannel ? currentTestChannel.id : undefined;

  const filteredModels = hasChannel
    ? currentTestChannel.models
      .split(',')
      .filter((model) =>
        model.toLowerCase().includes(modelSearchKeyword.toLowerCase())
      )
    : [];

  const handleCopySelected = () => {
    if (selectedModelKeys.length === 0) {
      showError(t('请先选择模型！'));
      return;
    }
    copy(selectedModelKeys.join(',')).then((ok) => {
      if (ok) {
        showSuccess(t('已复制 ${count} 个模型').replace('${count}', selectedModelKeys.length));
      } else {
        showError(t('复制失败，请手动复制'));
      }
    });
  };

  const handleSelectSuccess = () => {
    if (!currentTestChannel) return;
    const successKeys = currentTestChannel.models
      .split(',')
      .filter((m) => m.toLowerCase().includes(modelSearchKeyword.toLowerCase()))
      .filter((m) => {
        const res = modelTestResults[buildKey(channelId, m, TEST_KINDS.TEXT)];
        return res && res.success;
      });
    if (successKeys.length === 0) {
      showInfo(t('暂无成功模型'));
    }
    setSelectedModelKeys(successKeys);
  };

  const renderTestCell = (model, type) => {
    if (!hasChannel) return null;
    const key = buildKey(channelId, model, type);
    const running = testingKeys?.has?.(key);
    const result = modelTestResults?.[key];

    if (running) {
      return <Tag color='blue' shape='circle'>{t('测试中')}</Tag>;
    }
    if (!result) {
      return (
        <Button
          type='tertiary'
          size='small'
          onClick={() => testChannel(currentTestChannel, model, type)}
        >
          {t('测试')}
        </Button>
      );
    }
    return (
      <div className="flex items-center gap-2">
        <Tag color={result.success ? 'green' : 'red'} shape='circle'>
          {result.success ? t('成功') : t('失败')}
        </Tag>
        {result.success && (
          <Typography.Text type="tertiary">
            {t('请求时长: ${time}s').replace('${time}', Number(result.time).toFixed(2))}
          </Typography.Text>
        )}
        <Button
          size='small'
          type='tertiary'
          onClick={() => testChannel(currentTestChannel, model, type)}
        >
          {t('重测')}
        </Button>
      </div>
    );
  };

  const columns = [
    {
      title: t('模型名称'),
      dataIndex: 'model',
      render: (text) => (
        <div className="flex items-center">
          <Typography.Text strong>{text}</Typography.Text>
        </div>
      )
    },
    {
      title: (
        <Space spacing={6} align="center">
          <span>{t('Text')}</span>
          <Tooltip content={t('测试普通文本生成')}>
            <Tag size="small" type="ghost">i</Tag>
          </Tooltip>
        </Space>
      ),
      dataIndex: 'textStatus',
      render: (_, r) => renderTestCell(r.model, TEST_KINDS.TEXT),
    },
    {
      title: (
        <Space spacing={6} align="center">
          <span>{t('Function Call')}</span>
          <Tooltip content={t('测试函数调用/工具调用能力')}>
            <Tag size="small" type="ghost">i</Tag>
          </Tooltip>
        </Space>
      ),
      dataIndex: 'functionStatus',
      render: (_, r) => renderTestCell(r.model, TEST_KINDS.FUNCTION),
    },
    {
      title: (
        <Space spacing={6} align="center">
          <span>{t('JSON Valid')}</span>
          <Tooltip content={t('测试输出是否为合法 JSON')}>
            <Tag size="small" type="ghost">i</Tag>
          </Tooltip>
        </Space>
      ),
      dataIndex: 'jsonStatus',
      render: (_, r) => renderTestCell(r.model, TEST_KINDS.JSON),
    },
  ];

  const dataSource = (() => {
    if (!hasChannel) return [];
    const start = (modelTablePage - 1) * MODEL_TABLE_PAGE_SIZE;
    const end = start + MODEL_TABLE_PAGE_SIZE;
    return filteredModels.slice(start, end).map((model) => ({
      model,
      key: model,
    }));
  })();

  const footer = hasChannel ? (
    <div className="flex flex-col md:flex-row md:items-center gap-2 md:gap-3 md:justify-end w-full">
      {isBatchTesting ? (
        <Button type='danger' onClick={handleCloseModal}>
          {t('停止测试')}
        </Button>
      ) : (
        <Button type='tertiary' onClick={handleCloseModal}>
          {t('取消')}
        </Button>
      )}

      <Space>
        <Button
          onClick={() => batchTestModels(TEST_KINDS.TEXT)}
          loading={isBatchTesting}
          disabled={isBatchTesting}
        >
          {isBatchTesting ? t('测试中...') : t('批量测试 Text（${count}）').replace('${count}', filteredModels.length)}
        </Button>
        <Button
          onClick={() => batchTestModels(TEST_KINDS.FUNCTION)}
          loading={isBatchTesting}
          disabled={isBatchTesting}
        >
          {isBatchTesting ? t('测试中...') : t('批量测试 Function（${count}）').replace('${count}', filteredModels.length)}
        </Button>
        <Button
          onClick={() => batchTestModels(TEST_KINDS.JSON)}
          loading={isBatchTesting}
          disabled={isBatchTesting}
        >
          {isBatchTesting ? t('测试中...') : t('批量测试 JSON（${count}）').replace('${count}', filteredModels.length)}
        </Button>
      </Space>
    </div>
  ) : null;

  return (
    <Modal
      title={hasChannel ? (
        <div className="flex flex-col gap-2 w-full">
          <div className="flex items-center gap-2">
            <Typography.Text strong className="!text-[var(--semi-color-text-0)] !text-base">
              {currentTestChannel.name} {t('渠道的模型测试')}
            </Typography.Text>
            <Typography.Text type="tertiary" size="small">
              {t('共')} {currentTestChannel.models.split(',').length} {t('个模型')}
            </Typography.Text>
          </div>
        </div>
      ) : null}
      visible={showModelTestModal}
      onCancel={handleCloseModal}
      footer={footer}
      maskClosable={!isBatchTesting}
      className="!rounded-lg"
      size={isMobile ? 'full-width' : 'large'}
    >
      {hasChannel && (
        <div className="model-test-scroll">
          {/* 搜索与操作按钮 */}
          <div className="flex items-center justify-end gap-2 w-full mb-2">
            <Input
              placeholder={t('搜索模型...')}
              value={modelSearchKeyword}
              onChange={(v) => {
                setModelSearchKeyword(v);
                setModelTablePage(1);
              }}
              className="!w-full"
              prefix={<IconSearch />}
              showClear
            />

            <Button onClick={handleCopySelected}>
              {t('复制已选')}
            </Button>

            <Button type='tertiary' onClick={handleSelectSuccess}>
              {t('选择成功')}
            </Button>
          </div>

          <Table
            columns={columns}
            dataSource={dataSource}
            rowSelection={{
              selectedRowKeys: selectedModelKeys,
              onChange: (keys) => {
                if (allSelectingRef.current) {
                  allSelectingRef.current = false;
                  return;
                }
                setSelectedModelKeys(keys);
              },
              onSelectAll: (checked) => {
                allSelectingRef.current = true;
                setSelectedModelKeys(checked ? filteredModels : []);
              },
            }}
            pagination={{
              currentPage: modelTablePage,
              pageSize: MODEL_TABLE_PAGE_SIZE,
              total: filteredModels.length,
              showSizeChanger: false,
              onPageChange: (page) => setModelTablePage(page),
            }}
          />
        </div>
      )}
    </Modal>
  );
};

export default ModelTestModal;
