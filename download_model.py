import paramiko
import sys
import time

host = "192.168.0.3"
port = 2222
username = "koreaf16"
password = "Gnttkak1!"

try:
    client = paramiko.SSHClient()
    client.set_missing_host_key_policy(paramiko.AutoAddPolicy())
    print(f"Connecting to {host}:{port} as {username}...")
    client.connect(host, port=port, username=username, password=password, timeout=10)
    print("Connected successfully.")
    
    # Run a test command
    stdin, stdout, stderr = client.exec_command("echo 'SSH Works!'")
    print(stdout.read().decode())
    
    # Check for huggingface-cli
    stdin, stdout, stderr = client.exec_command("which huggingface-cli")
    hf_cli_path = stdout.read().decode().strip()
    if not hf_cli_path:
        print("huggingface-cli not found. Installing huggingface_hub...")
        client.exec_command("pip install -U huggingface_hub")
        hf_cli_path = "huggingface-cli"
    
    # Download the model in the background using nohup
    model_name = "Qwen/Qwen3.5-4B"
    cmd = f"nohup {hf_cli_path} download {model_name} > download_qwen.log 2>&1 & echo $!"
    print(f"Executing: {cmd}")
    stdin, stdout, stderr = client.exec_command(cmd)
    pid = stdout.read().decode().strip()
    print(f"Download started in background with PID: {pid}")
    print("You can check the progress by running: tail -f download_qwen.log")
    
    client.close()
except Exception as e:
    print(f"Failed to connect or execute command: {e}")
