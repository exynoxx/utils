using System;
using System.IO;
using System.Text;

class Splitter
{
    private static string _outputDir = Path.Combine(Directory.GetCurrentDirectory(), "out");
    
    static void Main(string[] args)
    {
        string? inputFile = null;
        int maxLines = int.MaxValue;
        int maxBytes = int.MaxValue;
        bool lineMode = true;

        // Parse arguments
        for (int i = 0; i < args.Length; i++)
        {
            switch (args[i])
            {
                case "-l":
                    lineMode = true;
                    if (i + 1 < args.Length && int.TryParse(args[++i], out int l))
                        maxLines = l;
                    break;
    
                case "-b":
                case "-m":
                    lineMode = false;
                    if (i + 1 < args.Length && int.TryParse(args[++i], out int m))
                        maxBytes = m * 1024 * 1024;
                    break;

                case "-h":
                case "--help":
                    ShowHelp();
                    return;
                default:
                    inputFile = Path.Combine(Directory.GetCurrentDirectory(), args[i]);
                    break;
            }
        }

        if (string.IsNullOrEmpty(inputFile) || !File.Exists(inputFile))
        {
            Console.WriteLine("Input file not found or not specified.");
            ShowHelp();
            return;
        }
        
        Directory.CreateDirectory(_outputDir);

        if (lineMode)
        {
            SplitByLine(inputFile, maxLines);
            return;
        }
            
        SplitByBytes(inputFile, maxBytes);
    }

    static void SplitByBytes(string file, int maxBytes = int.MaxValue)
    {
        var inputStream = File.OpenRead(file);
        var ext = Path.GetExtension(file);

        int fileIndex = 1;

        while (inputStream.Position < inputStream.Length)
        {
            var outputFile = $"Part_{fileIndex++:00}{ext}";
            Console.WriteLine($"Creating: {outputFile}");
            using var outputStream = new FileStream(Path.Combine(_outputDir, $"Part_{fileIndex++:00}{ext}"), FileMode.Create, FileAccess.Write);
            CopyBytes(inputStream, outputStream, maxBytes);
        }

        Console.WriteLine($"Done. Created {fileIndex - 1} file(s).");
    }
    
    static void CopyBytes(Stream input, Stream output, long bytes)
    {
        byte[] buffer = new byte[81920];

        while (bytes > 0)
        {
            int bytesRead = input.Read(buffer, 0, (int) Math.Min(buffer.Length, bytes));
            if (bytesRead == 0)
                break; // Reached end of stream before copying all requested bytes

            output.Write(buffer, 0, bytesRead);
            bytes -= bytesRead;
        }
    }


    static void SplitByLine(string file, int maxLines = int.MaxValue)
    {
        var ext = Path.GetExtension(file);

        using var reader = new StreamReader(file);
        int fileIndex = 1;
        int lineCount = 0;

        StreamWriter writer = CreateNewWriter(fileIndex++, ext);

        string? line;
        while ((line = reader.ReadLine()) != null)
        {
            if (lineCount >= maxLines)
            {
                writer.Dispose();
                writer = CreateNewWriter(fileIndex++, ext);
                lineCount = 0;
            }

            writer.WriteLine(line);
            lineCount++;
        }

        writer.Dispose();
        Console.WriteLine($"Done. Created {fileIndex - 1} file(s).");
    }

    static StreamWriter CreateNewWriter(int index, string ext)
    {
        var filename = Path.Combine(_outputDir, $"Part_{index:00}{ext}");
        Console.WriteLine($"Creating: {filename}");
        return new StreamWriter(filename, false, Encoding.UTF8);
    }

    static void ShowHelp()
    {
        Console.WriteLine(@"
Splitter
------------------
Usage:
  splitter.exe -i <input_file> [-l <lines>] [-m <megabytes>] [-h]

Options:
  -l, <number>      Max lines per output file (optional)
  -m, -b <size>     Max size per output file in MB (optional)
  -h, --help               Show this help message

Examples:
  splitter.exe -i bigfile.txt -l 500
      Split into chunks of 500 lines each.

  splitter.exe -i bigfile.txt -m 50
      Split into files no larger than 50MB.
");
    }
}
