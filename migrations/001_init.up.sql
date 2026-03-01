-- 上传任务表
CREATE TABLE IF NOT EXISTS upload_tasks (
    id SERIAL PRIMARY KEY,
    file_record_id INTEGER NOT NULL,
    status VARCHAR(20) NOT NULL,
    retry_count INTEGER DEFAULT 0,
    started_at TIMESTAMP WITH TIME ZONE,
    finished_at TIMESTAMP WITH TIME ZONE,
    error_msg TEXT,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_upload_tasks_file_record_id ON upload_tasks(file_record_id);
CREATE INDEX IF NOT EXISTS idx_upload_tasks_status ON upload_tasks(status);

-- 文件记录表
CREATE TABLE IF NOT EXISTS file_records (
    id SERIAL PRIMARY KEY,
    local_path VARCHAR(1024) NOT NULL,
    relative_path VARCHAR(1024) NOT NULL,
    remote_path VARCHAR(1024) NOT NULL,
    file_size BIGINT DEFAULT 0,
    file_hash VARCHAR(64),
    uploaded_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_file_records_local_path ON file_records(local_path);
CREATE INDEX IF NOT EXISTS idx_file_records_file_hash ON file_records(file_hash);

-- 上传日志表
CREATE TABLE IF NOT EXISTS upload_logs (
    id SERIAL PRIMARY KEY,
    task_id INTEGER NOT NULL,
    percent DECIMAL(5,2),
    bytes_done BIGINT DEFAULT 0,
    bytes_total BIGINT DEFAULT 0,
    speed BIGINT DEFAULT 0,
    message TEXT,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_upload_logs_task_id ON upload_logs(task_id);
