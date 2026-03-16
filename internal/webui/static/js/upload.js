(function() {
    const basePath = document.querySelector('meta[name="base-path"]')?.content || '';
    const dropzone = document.getElementById('upload-dropzone');
    const fileInput = document.getElementById('file-input');
    const secretCheckbox = document.getElementById('upload-secret');
    const statusDiv = document.getElementById('upload-status');
    
    // Drag & drop
    dropzone.addEventListener('dragover', (e) => {
        e.preventDefault();
        dropzone.classList.add('dragover');
    });
    dropzone.addEventListener('dragleave', () => dropzone.classList.remove('dragover'));
    dropzone.addEventListener('drop', (e) => {
        e.preventDefault();
        dropzone.classList.remove('dragover');
        uploadFiles(e.dataTransfer.files);
    });
    
    // File input
    fileInput.addEventListener('change', () => uploadFiles(fileInput.files));
    dropzone.addEventListener('click', (e) => {
        if (e.target !== fileInput && e.target.tagName !== 'LABEL') fileInput.click();
    });
    
    function uploadFiles(files) {
        if (!files || files.length === 0) return;
        showLoading();
        const formData = new FormData();
        for (const file of files) formData.append('files', file);
        if (secretCheckbox && secretCheckbox.checked) {
            formData.append('secret', 'true');
        }
        
        fetch(basePath + '/api/upload', { method: 'POST', body: formData })
            .then(res => res.json())
            .then(data => showResults(data))
            .catch(err => showError(err.message));
    }
    
    function showLoading() {
        statusDiv.innerHTML = '<div class="upload-loading">アップロード中...</div>';
    }
    
    function showResults(data) {
        const summary = `${data.saved_count} 件保存、${data.rejected_count} 件拒否`;
        let html = `<p>${summary}</p><div class="upload-result">`;
        for (const item of data.results) {
            html += `<div class="upload-result-item ${item.status}">
                ${item.file_name}: ${item.message}
            </div>`;
        }
        if (data.vectorize_triggered) html += '<p>ベクトル化を開始しました</p>';
        html += '</div>';
        statusDiv.innerHTML = html;
    }
    
    function showError(message) {
        statusDiv.innerHTML = `<div class="upload-result-item error">エラー: ${message}</div>`;
    }
})();
