import React, { useEffect, useState } from 'react';
import { API, showError } from '../../helpers';
import { Typography, Spin, List, Tag, Space, Button } from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';

const ChannelCacheViewer = ({ channelId }) => {
    const { t } = useTranslation();

    const [loading, setLoading] = useState(true);
    const [caches, setCaches] = useState([]);

    useEffect(() => {
        const fetchCaches = async () => {
            try {
                const res = await API.get(`/api/channel/${channelId}/caches`);
                if (res?.data?.caches) {
                    setCaches(res.data.caches);
                }
            } catch (err) {
                showError(`${t('Error loading caches:')} ${err.message}`);
            } finally {
                setLoading(false);
            }
        };

        fetchCaches();
    }, [channelId]);

    if (loading) return <Spin />;

    return (
        <div>
            <Typography.Text strong>{t('Caches found:')} {caches.length}</Typography.Text>
            <List
                dataSource={caches}
                renderItem={(item) => {
                    const [key, value] = item.split(' => ');
                    return (
                        <List.Item>
                            <Space vertical align="start">
                                <Typography.Text type="secondary">{key}</Typography.Text>
                                <Tag color="light-green">{value}</Tag>
                            </Space>
                        </List.Item>
                    );
                }}
            />
        </div>
    );
};

export default ChannelCacheViewer;
