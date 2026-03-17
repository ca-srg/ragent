(function() {
    const basePath = document.querySelector('meta[name="base-path"]')?.content || '';
    const dropzone = document.getElementById('upload-dropzone');
    const fileInput = document.getElementById('file-input');
    const secretCheckbox = document.getElementById('upload-secret');
    const statusDiv = document.getElementById('upload-status');
    const fileListDiv = document.getElementById('upload-file-list');
    const controlsDiv = document.getElementById('upload-controls');
    const submitBtn = document.getElementById('upload-submit-btn');

    let pendingFiles = null;

    // Drag & drop
    dropzone.addEventListener('dragover', (e) => {
        e.preventDefault();
        dropzone.classList.add('dragover');
    });
    dropzone.addEventListener('dragleave', () => dropzone.classList.remove('dragover'));
    dropzone.addEventListener('drop', (e) => {
        e.preventDefault();
        dropzone.classList.remove('dragover');
        stageFiles(e.dataTransfer.files);
    });

    fileInput.addEventListener('change', () => stageFiles(fileInput.files));

    dropzone.addEventListener('click', (e) => {
        if (e.target !== fileInput && e.target.tagName !== 'LABEL') fileInput.click();
    });

    submitBtn.addEventListener('click', () => {
        if (pendingFiles && pendingFiles.length > 0) {
            uploadFiles(pendingFiles);
        }
    });

    function stageFiles(files) {
        if (!files || files.length === 0) return;
        pendingFiles = files;
        showFileList(files);
    }

    function showFileList(files) {
        let html = '<ul>';
        for (const file of files) {
            const size = file.size < 1024 * 1024
                ? (file.size / 1024).toFixed(1) + ' KB'
                : (file.size / (1024 * 1024)).toFixed(1) + ' MB';
            html += '<li><span class="upload-file-name">' + escapeHtml(file.name) + '</span>'
                  + '<span class="upload-file-size">' + size + '</span></li>';
        }
        html += '</ul>';
        fileListDiv.innerHTML = html;
        fileListDiv.style.display = 'block';
        controlsDiv.style.display = 'flex';
        statusDiv.innerHTML = '';
    }

    function uploadFiles(files) {
        showLoading();
        submitBtn.disabled = true;
        const formData = new FormData();
        for (const file of files) formData.append('files', file);
        if (secretCheckbox && secretCheckbox.checked) {
            formData.append('secret', 'true');
        }

        fetch(basePath + '/api/upload', { method: 'POST', body: formData })
            .then(res => res.json())
            .then(data => {
                showResults(data);
                resetStage();
            })
            .catch(err => {
                showError(err.message);
                submitBtn.disabled = false;
            });
    }

    function resetStage() {
        pendingFiles = null;
        fileInput.value = '';
        fileListDiv.style.display = 'none';
        controlsDiv.style.display = 'none';
        submitBtn.disabled = false;
    }

    function showLoading() {
        statusDiv.innerHTML = '<div class="upload-loading">アップロード中...</div>';
    }

    function showResults(data) {
        const summary = data.saved_count + ' 件保存、' + data.rejected_count + ' 件拒否';
        let html = '<p>' + summary + '</p><div class="upload-result">';
        for (const item of data.results) {
            html += '<div class="upload-result-item ' + item.status + '">'
                 + escapeHtml(item.file_name) + ': ' + escapeHtml(item.message)
                 + '</div>';
        }
        if (data.vectorize_triggered) html += '<p>ベクトル化を開始しました</p>';
        html += '</div>';
        statusDiv.innerHTML = html;
    }

    function showError(message) {
        statusDiv.innerHTML = '<div class="upload-result-item error">エラー: ' + escapeHtml(message) + '</div>';
    }

    function escapeHtml(text) {
        const div = document.createElement('div');
        div.appendChild(document.createTextNode(text));
        return div.innerHTML;
    }
})();
