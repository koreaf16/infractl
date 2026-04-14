import paramiko
import time

host = '192.168.0.3'
port = 2222
username = 'koreaf16'
password = 'Gnttkak1!'

try:
    client = paramiko.SSHClient()
    client.set_missing_host_key_policy(paramiko.AutoAddPolicy())
    client.connect(host, port=port, username=username, password=password, timeout=10)

    # 1. ConfigMap 가져오기 및 formats 설정 추가
    # settings.yml 내부의 search: 섹션에 formats: [html, json] 추가
    # sed를 사용하여 'search:' 문자열 다음 줄에 설정을 삽입합니다.
    # (indented 4 spaces for settings.yml content, so we need 6 spaces for formats)
    patch_cmd = """
    kubectl get configmap searxng-config -n searxng -o yaml > /tmp/searxng_cm.yaml
    # 이미 존재하는지 확인 후 없으면 추가
    if ! grep -q "formats: \[html, json\]" /tmp/searxng_cm.yaml; then
        sed -i '/    search:/a \    \    formats: [html, json]' /tmp/searxng_cm.yaml
        kubectl apply -f /tmp/searxng_cm.yaml
        echo "ConfigMap updated with JSON format support."
    else
        echo "JSON format support already exists in ConfigMap."
    fi
    
    # 2. Deployment 재시작
    kubectl rollout restart deployment searxng -n searxng
    echo "Restarting SearXNG deployment..."
    
    # 3. 롤아웃 대기
    kubectl rollout status deployment searxng -n searxng --timeout=60s
    """

    print(f"Connecting to {host}...")
    stdin, stdout, stderr = client.exec_command(patch_cmd)
    
    out = stdout.read().decode()
    err = stderr.read().decode()
    
    if out: print("STDOUT:", out)
    if err: print("STDERR:", err)

    client.close()
    print("Done.")

except Exception as e:
    print(f"Error: {e}")
