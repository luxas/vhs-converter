@echo off
for /f "tokens=2 delims==" %%a in ('wmic OS Get localdatetime /value') do set "dt=%%a"
set "YY=%dt:~2,2%" & set "YYYY=%dt:~0,4%" & set "MM=%dt:~4,2%" & set "DD=%dt:~6,2%"
set "HH=%dt:~8,2%" & set "Min=%dt:~10,2%" & set "Sec=%dt:~12,2%"

set "datestamp=%YYYY%%MM%%DD%" & set "timestamp=%HH%%Min%%Sec%"
set "fullstamp=%YYYY%-%MM%-%DD%_%HH%-%Min%-%Sec%"
@echo on

if not exist "E:\Recordings" mkdir "E:\Recordings"
mkdir "E:\Recordings/%fullstamp%"

"C:\Program Files\luxas_ffmpeg\ffmpeg.exe" -hwaccel qsv -f decklink -video_input hdmi -raw_format bgra -i "Intensity Pro 4K" -c:v h264_qsv -r 30 -c:a mp2 -global_quality 20 -f tee -map 0 "[f=segment:reset_timestamps=1:segment_time=600]E:\Recordings/%fullstamp%/output%%03d.mp4|[f=mpegts]udp://localhost:6666"
